// Package vpnfake runs in-memory VPN panels that speak the APIs of
// Marzban, PasarGuard, Marzneshin, Remnawave and Hiddify (X-UI types use
// xuifake). Tests use them, and so does the builder's test chat, which
// must never create users on a real panel.
package vpnfake

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mrjvadi/tgcreator/internal/workflow"
	"github.com/mrjvadi/tgcreator/internal/xui/xuifake"
)

// Panel is a fake panel of one type.
type Panel struct {
	Type string
	cfg  workflow.VPNPanel
	xui  *xuifake.Panel
	srv  *httptest.Server

	mu       sync.Mutex
	users    map[string]*user
	nextID   int
	tokens   map[string]bool
	logins   int
	Requests []string
}

type user struct {
	ID       int
	UUID     string
	Name     string
	Status   string // marzban family: active, disabled, limited, expired, on_hold
	Enabled  bool
	Used     int64
	Limit    int64
	Expire   time.Time     // zero = never
	OnHold   time.Duration // pending first use
	Note     string
	Tg       int64
	Groups   []string
	Online   time.Time
	Key      string
	HWID     int
	ResetAt  int
	Protocol string
}

const adminUUID = "4a2f6c1e-8d3b-4f5a-9c7e-1b2d3e4f5a6b"

// New starts a fake panel. X-UI types are served by xuifake.
func New(typ string) *Panel {
	p := &Panel{Type: typ, users: map[string]*user{}, nextID: 1, tokens: map[string]bool{}}
	switch typ {
	case "", "3x-ui", "x-ui":
		p.xui = xuifake.New(typ)
		p.Type = p.xui.Type
		p.cfg = workflow.VPNPanel{Type: p.Type, URL: p.xui.URL, Username: p.xui.Username, Password: p.xui.Password}
		return p
	}
	p.srv = httptest.NewServer(http.HandlerFunc(p.serve))
	p.cfg = workflow.VPNPanel{Type: typ, URL: p.srv.URL + "/", Username: "admin", Password: "admin"}
	switch typ {
	case "remnawave":
		p.cfg.Username, p.cfg.Password, p.cfg.Token = "", "", "remna-api-token"
	case "hiddify":
		p.cfg.Username, p.cfg.Password = "", ""
		p.cfg.URL = p.srv.URL + "/hpath/" + adminUUID + "/admin/"
		p.cfg.SubURL = "https://vpn.example.com/cpath/"
	case "marzban", "pasarguard":
		p.cfg.URL = p.srv.URL + "/dashboard/"
	}
	return p
}

// Config is how a workflow connects to this panel.
func (p *Panel) Config() workflow.VPNPanel { return p.cfg }

// Close stops the server.
func (p *Panel) Close() {
	if p.xui != nil {
		p.xui.Close()
		return
	}
	p.srv.Close()
}

// Logins counts successful logins.
func (p *Panel) Logins() int {
	if p.xui != nil {
		return p.xui.Logins
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.logins
}

// ExpireSessions forgets every login, like a panel restart.
func (p *Panel) ExpireSessions() {
	if p.xui != nil {
		p.xui.ExpireSessions()
		return
	}
	p.mu.Lock()
	p.tokens = map[string]bool{}
	p.mu.Unlock()
}

// SetUsage sets a user's used traffic.
func (p *Panel) SetUsage(name string, used int64) {
	if p.xui != nil {
		p.xui.SetUsage(name, used, 0)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if u := p.byName(name); u != nil {
		u.Used = used
		p.refresh(u)
	}
}

// SetOnline marks users as connected now.
func (p *Panel) SetOnline(names ...string) {
	if p.xui != nil {
		p.xui.SetOnline(names...)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, n := range names {
		if u := p.byName(n); u != nil {
			u.Online = time.Now()
		}
	}
}

// Exists reports whether a user exists.
func (p *Panel) Exists(name string) bool {
	if p.xui != nil {
		c, _ := p.xui.Client(name)
		return c != nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.byName(name) != nil
}

// Limit returns a user's traffic limit in bytes and expiry.
func (p *Panel) Limit(name string) (limit int64, expire time.Time, enabled bool) {
	if p.xui != nil {
		c, _ := p.xui.Client(name)
		if c == nil {
			return 0, time.Time{}, false
		}
		f, _ := c["totalGB"].(float64)
		e, _ := c["expiryTime"].(float64)
		en, _ := c["enable"].(bool)
		if e > 0 {
			return int64(f), time.UnixMilli(int64(e)), en
		}
		return int64(f), time.Time{}, en
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	u := p.byName(name)
	if u == nil {
		return 0, time.Time{}, false
	}
	if p.Type == "hiddify" { // Expire holds start_date, OnHold the package
		if u.Expire.IsZero() || u.OnHold >= 10000*24*time.Hour {
			return u.Limit, time.Time{}, u.Enabled
		}
		return u.Limit, u.Expire.Add(u.OnHold), u.Enabled
	}
	return u.Limit, u.Expire, p.enabled(u)
}

func (p *Panel) byName(name string) *user {
	if u := p.users[p.key(name)]; u != nil {
		return u
	}
	for _, u := range p.users {
		if u.Name == name {
			return u
		}
	}
	return nil
}

func (p *Panel) key(name string) string {
	if p.Type == "marzneshin" {
		return strings.ToLower(name)
	}
	return name
}

func (p *Panel) enabled(u *user) bool {
	if p.Type == "marzban" || p.Type == "pasarguard" {
		return u.Status != "disabled"
	}
	return u.Enabled
}

// refresh recomputes a Marzban-style status the way the panel's jobs do.
func (p *Panel) refresh(u *user) {
	if u.Status == "disabled" || u.Status == "on_hold" {
		return
	}
	switch {
	case !u.Expire.IsZero() && u.Expire.Before(time.Now()):
		u.Status = "expired"
	case u.Limit > 0 && u.Used >= u.Limit:
		u.Status = "limited"
	default:
		u.Status = "active"
	}
}

func token() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func uuid() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func (p *Panel) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Requests = append(p.Requests, r.Method+" "+r.URL.Path)
	switch p.Type {
	case "marzban", "pasarguard", "marzneshin":
		p.serveMarz(w, r)
	case "remnawave":
		p.serveRemna(w, r)
	case "hiddify":
		p.serveHiddify(w, r)
	default:
		http.NotFound(w, r)
	}
}

func decode(r *http.Request) map[string]any {
	var m map[string]any
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &m)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

func f64(v any) float64 {
	f, _ := v.(float64)
	return f
}

func iso(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format("2006-01-02T15:04:05")
}

// ---------- Marzban / PasarGuard / Marzneshin ----------

var marzInbounds = map[string][]map[string]any{
	"vless": {{"tag": "VLESS TCP REALITY", "protocol": "vless", "network": "tcp", "tls": "reality", "port": 443}},
	"vmess": {{"tag": "VMess WS", "protocol": "vmess", "network": "ws", "tls": "tls", "port": 8443}},
}

func (p *Panel) serveMarz(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api")
	tokenPath := "/admin/token"
	userBase, usersPath := "/user/", "/users"
	if p.Type == "marzneshin" {
		tokenPath, userBase = "/admins/token", "/users/"
	}
	if r.Method == http.MethodPost && path == tokenPath {
		_ = r.ParseForm()
		if r.PostForm.Get("username") != p.cfg.Username || r.PostForm.Get("password") != p.cfg.Password {
			writeJSON(w, 401, map[string]any{"detail": "Incorrect username or password"})
			return
		}
		t := token()
		p.tokens[t] = true
		p.logins++
		writeJSON(w, 200, map[string]any{"access_token": t, "token_type": "bearer"})
		return
	}
	if !p.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] {
		writeJSON(w, 401, map[string]any{"detail": "Could not validate credentials"})
		return
	}
	switch {
	case r.Method == http.MethodGet && path == "/inbounds" && p.Type == "marzban":
		writeJSON(w, 200, marzInbounds)
	case r.Method == http.MethodGet && path == "/groups" && p.Type == "pasarguard":
		writeJSON(w, 200, map[string]any{"groups": []any{
			map[string]any{"id": 1, "name": "Main", "inbound_tags": []string{"VLESS TCP REALITY"}, "is_disabled": false, "total_users": len(p.users)},
			map[string]any{"id": 2, "name": "Old", "inbound_tags": []string{}, "is_disabled": true, "total_users": 0},
		}, "total": 2})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/services") && p.Type == "marzneshin":
		writeJSON(w, 200, map[string]any{"items": []any{map[string]any{"id": 1, "name": "Main", "inbound_ids": []int{1, 2}, "user_ids": []int{}}}, "total": 1, "page": 1, "size": 100, "pages": 1})
	case r.Method == http.MethodPost && (path == "/user" || path == "/users" && p.Type == "marzneshin"):
		p.marzCreate(w, decode(r))
	case r.Method == http.MethodGet && path == usersPath:
		p.marzList(w, r.URL.Query())
	case strings.HasPrefix(path, userBase):
		rest := strings.TrimPrefix(path, userBase)
		name, action, _ := strings.Cut(rest, "/")
		name, _ = url.PathUnescape(name)
		u := p.users[p.key(name)]
		if u == nil {
			writeJSON(w, 404, map[string]any{"detail": "User not found"})
			return
		}
		switch {
		case r.Method == http.MethodGet && action == "":
			writeJSON(w, 200, p.marzJSON(u))
		case r.Method == http.MethodPut && action == "":
			if err := p.marzModify(u, decode(r)); err != nil {
				writeJSON(w, 400, map[string]any{"detail": err.Error()})
				return
			}
			writeJSON(w, 200, p.marzJSON(u))
		case r.Method == http.MethodDelete && action == "":
			delete(p.users, p.key(name))
			if p.Type == "pasarguard" {
				writeJSON(w, 204, nil)
				return
			}
			writeJSON(w, 200, map[string]any{"detail": "User successfully deleted"})
		case r.Method == http.MethodPost && action == "reset":
			u.Used = 0
			if u.Status == "limited" {
				u.Status = "active"
			}
			writeJSON(w, 200, p.marzJSON(u))
		case r.Method == http.MethodPost && (action == "enable" || action == "disable") && p.Type == "marzneshin":
			u.Enabled = action == "enable"
			writeJSON(w, 200, p.marzJSON(u))
		default:
			writeJSON(w, 404, map[string]any{"detail": "Not Found"})
		}
	default:
		writeJSON(w, 404, map[string]any{"detail": "Not Found"})
	}
}

func (p *Panel) marzCreate(w http.ResponseWriter, b map[string]any) {
	name, _ := b["username"].(string)
	if p.Type == "marzneshin" {
		name = strings.ToLower(name)
	}
	if p.users[p.key(name)] != nil {
		writeJSON(w, 409, map[string]any{"detail": "User already exists"})
		return
	}
	u := &user{ID: p.nextID, UUID: uuid(), Name: name, Status: "active", Enabled: true, Limit: int64(f64(b["data_limit"])), Note: fmt.Sprint(b["note"]), Key: token()}
	if b["note"] == nil {
		u.Note = ""
	}
	p.nextID++
	switch p.Type {
	case "marzban":
		proxies, _ := b["proxies"].(map[string]any)
		if len(proxies) == 0 {
			writeJSON(w, 422, map[string]any{"detail": "Each user needs at least one proxy"})
			return
		}
		for _, proto := range []string{"vless", "vmess"} {
			if _, ok := proxies[proto]; ok && u.Protocol == "" {
				u.Protocol = proto
			}
		}
		if ins, ok := b["inbounds"].(map[string]any); ok {
			for _, tags := range ins {
				for _, t := range tags.([]any) {
					u.Groups = append(u.Groups, t.(string))
				}
			}
		} else {
			for proto := range proxies {
				for _, in := range marzInbounds[proto] {
					u.Groups = append(u.Groups, in["tag"].(string))
				}
			}
		}
		sort.Strings(u.Groups)
	case "pasarguard":
		ids, _ := b["group_ids"].([]any)
		if len(ids) == 0 {
			writeJSON(w, 422, map[string]any{"detail": "you must select at least one group"})
			return
		}
		for _, id := range ids {
			u.Groups = append(u.Groups, strconv.Itoa(int(f64(id))))
		}
		u.HWID = int(f64(b["hwid_limit"]))
	case "marzneshin":
		for _, id := range b["service_ids"].([]any) {
			u.Groups = append(u.Groups, strconv.Itoa(int(f64(id))))
		}
	}
	if p.Type == "marzneshin" {
		if err := p.neshinExpiry(u, b); err != nil {
			writeJSON(w, 422, map[string]any{"detail": err.Error()})
			return
		}
	} else {
		if e := f64(b["expire"]); e > 0 {
			u.Expire = time.Unix(int64(e), 0)
		}
		if b["status"] == "on_hold" {
			if u.Expire.IsZero() == false {
				writeJSON(w, 422, map[string]any{"detail": "User cannot be on hold with specified expire."})
				return
			}
			u.Status, u.OnHold = "on_hold", time.Duration(f64(b["on_hold_expire_duration"]))*time.Second
		}
	}
	p.users[p.key(name)] = u
	writeJSON(w, 200, p.marzJSON(u))
}

func (p *Panel) neshinExpiry(u *user, b map[string]any) error {
	switch b["expire_strategy"] {
	case "never":
		u.Expire, u.OnHold = time.Time{}, 0
	case "fixed_date":
		t, err := time.Parse(time.RFC3339, fmt.Sprint(b["expire_date"]))
		if err != nil {
			return fmt.Errorf("User expire_strategy cannot be fixed_date without a valid expire date.")
		}
		u.Expire, u.OnHold = t, 0
	case "start_on_first_use":
		if f64(b["usage_duration"]) <= 0 {
			return fmt.Errorf("start_on_first_use without a valid usage_duration")
		}
		u.Expire, u.OnHold = time.Time{}, time.Duration(f64(b["usage_duration"]))*time.Second
	case nil:
	default:
		return fmt.Errorf("bad expire_strategy")
	}
	return nil
}

func (p *Panel) marzModify(u *user, b map[string]any) error {
	if p.Type == "marzneshin" {
		if v, ok := b["data_limit"]; ok && v != nil {
			u.Limit = int64(f64(v))
		}
		if n, ok := b["note"].(string); ok {
			u.Note = n
		}
		return p.neshinExpiry(u, b)
	}
	if s, ok := b["status"].(string); ok {
		u.Status = s
		if s != "on_hold" {
			u.OnHold = 0
		}
	}
	if v, ok := b["data_limit"]; ok && v != nil {
		u.Limit = int64(f64(v))
	}
	if v, ok := b["expire"]; ok && v != nil {
		if e := f64(v); e > 0 {
			u.Expire = time.Unix(int64(e), 0)
		} else {
			u.Expire = time.Time{}
		}
	}
	if v, ok := b["on_hold_expire_duration"]; ok && v != nil {
		u.OnHold = time.Duration(f64(v)) * time.Second
	}
	if n, ok := b["note"].(string); ok {
		u.Note = n
	}
	if v, ok := b["hwid_limit"]; ok && v != nil {
		u.HWID = int(f64(v))
	}
	p.refresh(u)
	return nil
}

func (p *Panel) marzJSON(u *user) map[string]any {
	var limit any
	if u.Limit > 0 {
		limit = u.Limit
	}
	m := map[string]any{"username": u.Name, "used_traffic": u.Used, "data_limit": limit, "note": u.Note, "online_at": iso(u.Online)}
	switch p.Type {
	case "marzban":
		var exp any
		if !u.Expire.IsZero() {
			exp = u.Expire.Unix()
		}
		ins := map[string][]string{}
		for _, t := range u.Groups {
			for proto, list := range marzInbounds {
				for _, in := range list {
					if in["tag"] == t {
						ins[proto] = append(ins[proto], t)
					}
				}
			}
		}
		m["status"], m["expire"], m["inbounds"] = u.Status, exp, ins
		m["on_hold_expire_duration"] = int64(u.OnHold.Seconds())
		m["proxies"] = map[string]any{u.Protocol: map[string]any{"id": u.UUID, "flow": ""}}
		m["links"] = []string{"vless://" + u.UUID + "@vpn.example.com:443?security=reality#" + u.Name}
		m["subscription_url"] = "/sub/" + u.Key
	case "pasarguard":
		m["status"], m["expire"], m["group_ids"] = u.Status, iso(u.Expire), u.Groups
		m["on_hold_expire_duration"] = int64(u.OnHold.Seconds())
		m["proxy_settings"] = map[string]any{"vless": map[string]any{"id": u.UUID}, "trojan": map[string]any{"password": u.Key}}
		m["subscription_url"] = "https://sub.example.com/sub/" + u.Key
		m["hwid_limit"] = u.HWID
		if u.HWID == 0 {
			m["hwid_limit"] = nil
		}
	case "marzneshin":
		strategy := "never"
		switch {
		case !u.Expire.IsZero():
			strategy = "fixed_date"
		case u.OnHold > 0:
			strategy = "start_on_first_use"
		}
		m["key"], m["enabled"], m["expire_strategy"], m["expire_date"] = u.Key, u.Enabled, strategy, iso(u.Expire)
		m["usage_duration"] = nil
		if u.OnHold > 0 {
			m["usage_duration"] = int64(u.OnHold.Seconds())
		}
		m["expired"] = !u.Expire.IsZero() && u.Expire.Before(time.Now())
		m["data_limit_reached"] = u.Limit > 0 && u.Used >= u.Limit
		m["is_active"] = u.Enabled && m["expired"] == false && m["data_limit_reached"] == false
		m["activated"] = m["is_active"]
		m["subscription_url"] = "/sub/" + u.Name + "/" + u.Key
		ids := []int{}
		for _, g := range u.Groups {
			n, _ := strconv.Atoi(g)
			ids = append(ids, n)
		}
		m["service_ids"] = ids
	}
	return m
}

func (p *Panel) sortedUsers() []*user {
	list := make([]*user, 0, len(p.users))
	for _, u := range p.users {
		list = append(list, u)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}

func (p *Panel) marzList(w http.ResponseWriter, q url.Values) {
	var match []any
	search := strings.ToLower(q.Get("search"))
	if p.Type == "marzneshin" {
		search = strings.ToLower(q.Get("username"))
	}
	for _, u := range p.sortedUsers() {
		if search != "" && !strings.Contains(strings.ToLower(u.Name), search) && (p.Type == "marzneshin" || !strings.Contains(strings.ToLower(u.Note), search)) {
			continue
		}
		match = append(match, p.marzJSON(u))
	}
	if p.Type == "marzneshin" {
		size, _ := strconv.Atoi(q.Get("size"))
		pg, _ := strconv.Atoi(q.Get("page"))
		size, pg = max(size, 1), max(pg, 1)
		pages := (len(match) + size - 1) / size
		lo, hi := min((pg-1)*size, len(match)), min(pg*size, len(match))
		writeJSON(w, 200, map[string]any{"items": nonNil(match[lo:hi]), "total": len(match), "page": pg, "size": size, "pages": pages})
		return
	}
	off, _ := strconv.Atoi(q.Get("offset"))
	lim, _ := strconv.Atoi(q.Get("limit"))
	if lim == 0 {
		lim = len(match)
	}
	lo, hi := min(off, len(match)), min(off+lim, len(match))
	writeJSON(w, 200, map[string]any{"users": nonNil(match[lo:hi]), "total": len(match)})
}

func nonNil(l []any) []any {
	if l == nil {
		return []any{}
	}
	return l
}

// ---------- Remnawave ----------

var remnaSquads = []map[string]any{
	{"uuid": "0f1e2d3c-4b5a-4968-8776-655443322110", "name": "Default-Squad"},
	{"uuid": "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d", "name": "Germany"},
}

func (p *Panel) serveRemna(w http.ResponseWriter, r *http.Request) {
	fail := func(status int, msg string) {
		writeJSON(w, status, map[string]any{"message": msg, "statusCode": status})
	}
	if r.Header.Get("X-Forwarded-Proto") != "https" {
		fail(403, "Reverse proxy and HTTPS are required.")
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+p.cfg.Token {
		fail(401, "Unauthorized")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api")
	ok := func(v any) { writeJSON(w, 200, map[string]any{"response": v}) }
	switch {
	case r.Method == http.MethodGet && path == "/internal-squads":
		list := []any{}
		for _, sq := range remnaSquads {
			n := 0
			for _, u := range p.users {
				for _, g := range u.Groups {
					if g == sq["uuid"] {
						n++
					}
				}
			}
			list = append(list, map[string]any{"uuid": sq["uuid"], "name": sq["name"], "info": map[string]any{"membersCount": n, "inboundsCount": 1}})
		}
		ok(map[string]any{"total": len(list), "internalSquads": list})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/users/by-username/"):
		name, _ := url.PathUnescape(strings.TrimPrefix(path, "/users/by-username/"))
		if u := p.users[name]; u != nil {
			ok(p.remnaJSON(u))
			return
		}
		fail(404, "User not found")
	case r.Method == http.MethodGet && path == "/users":
		start, _ := strconv.Atoi(r.URL.Query().Get("start"))
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		if size < 1 || size > 1000 {
			fail(400, "Size (limit) must be less than 1000")
			return
		}
		all := p.sortedUsers()
		var list []any
		for _, u := range all[min(start, len(all)):min(start+size, len(all))] {
			list = append(list, p.remnaJSON(u))
		}
		ok(map[string]any{"users": nonNil(list), "total": len(all)})
	case r.Method == http.MethodPost && path == "/users":
		b := decode(r)
		name, _ := b["username"].(string)
		if p.users[name] != nil {
			fail(400, "User username already exists")
			return
		}
		exp, err := time.Parse(time.RFC3339, fmt.Sprint(b["expireAt"]))
		if err != nil {
			fail(400, "expireAt: Invalid ISO datetime")
			return
		}
		u := &user{ID: p.nextID, UUID: uuid(), Name: name, Enabled: b["status"] != "DISABLED", Limit: int64(f64(b["trafficLimitBytes"])), Expire: exp,
			Note: str(b["description"]), Tg: int64(f64(b["telegramId"])), HWID: int(f64(b["hwidDeviceLimit"])), Key: token()[:16]}
		for _, g := range asList(b["activeInternalSquads"]) {
			u.Groups = append(u.Groups, g.(string))
		}
		p.nextID++
		p.users[name] = u
		ok(p.remnaJSON(u))
	case r.Method == http.MethodPatch && path == "/users":
		b := decode(r)
		name, _ := b["username"].(string)
		u := p.users[name]
		if u == nil {
			fail(404, "User not found")
			return
		}
		if v, ok := b["trafficLimitBytes"]; ok {
			u.Limit = int64(f64(v))
		}
		if v, ok := b["expireAt"]; ok {
			t, err := time.Parse(time.RFC3339, fmt.Sprint(v))
			if err != nil || !t.After(time.Now()) {
				fail(400, "Expiration date cannot be in the past")
				return
			}
			u.Expire = t
		}
		if s, ok := b["status"].(string); ok {
			u.Enabled = s != "DISABLED"
		}
		if d, ok := b["description"].(string); ok {
			u.Note = d
		}
		if v, ok := b["hwidDeviceLimit"]; ok {
			u.HWID = int(f64(v))
		}
		ok(p.remnaJSON(u))
	case strings.HasPrefix(path, "/users/"):
		rest := strings.TrimPrefix(path, "/users/")
		id, action, _ := strings.Cut(rest, "/")
		var u *user
		for _, x := range p.users {
			if strconv.Itoa(x.ID) == id {
				u = x
			}
		}
		if u == nil {
			fail(404, "User not found")
			return
		}
		switch {
		case r.Method == http.MethodDelete && action == "":
			delete(p.users, u.Name)
			ok(map[string]any{"isDeleted": true})
		case r.Method == http.MethodPost && action == "actions/reset-traffic":
			u.Used = 0
			ok(p.remnaJSON(u))
		default:
			fail(404, "Not Found")
		}
	default:
		fail(404, "Not Found")
	}
}

func (p *Panel) remnaJSON(u *user) map[string]any {
	status := "ACTIVE"
	switch {
	case !u.Enabled:
		status = "DISABLED"
	case u.Expire.Before(time.Now()):
		status = "EXPIRED"
	case u.Limit > 0 && u.Used >= u.Limit:
		status = "LIMITED"
	}
	squads := []any{}
	for _, g := range u.Groups {
		for _, sq := range remnaSquads {
			if sq["uuid"] == g {
				squads = append(squads, map[string]any{"uuid": g, "name": sq["name"]})
			}
		}
	}
	var tg, hwid any
	if u.Tg != 0 {
		tg = u.Tg
	}
	if u.HWID != 0 {
		hwid = u.HWID
	}
	var online any
	if !u.Online.IsZero() {
		online = u.Online.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"id": u.ID, "shortUuid": u.Key, "username": u.Name, "status": status, "trafficLimitBytes": u.Limit, "trafficLimitStrategy": "NO_RESET",
		"expireAt": u.Expire.UTC().Format(time.RFC3339), "telegramId": tg, "description": u.Note, "hwidDeviceLimit": hwid, "vlessUuid": u.UUID,
		"subscriptionUrl": "https://sub.example.com/" + u.Key, "activeInternalSquads": squads,
		"userTraffic": map[string]any{"usedTrafficBytes": u.Used, "lifetimeUsedTrafficBytes": u.Used, "onlineAt": online},
	}
}

// ---------- Hiddify ----------

func (p *Panel) serveHiddify(w http.ResponseWriter, r *http.Request) {
	path, found := strings.CutPrefix(r.URL.Path, "/hpath/api/v2/admin")
	if !found {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Hiddify-API-Key") != adminUUID {
		writeJSON(w, 401, map[string]any{"message": "Unauthorized"})
		return
	}
	byUUID := func(id string) *user {
		for _, u := range p.users {
			if u.UUID == id {
				return u
			}
		}
		return nil
	}
	switch {
	case r.Method == http.MethodGet && path == "/user/":
		if len(p.users) == 0 {
			writeJSON(w, 404, map[string]any{"message": "You have no user"})
			return
		}
		var list []any
		for _, u := range p.sortedUsers() {
			list = append(list, p.hidJSON(u))
		}
		writeJSON(w, 200, list)
	case r.Method == http.MethodPost && path == "/user/":
		b := decode(r)
		id, _ := b["uuid"].(string)
		if id == "" {
			id = uuid()
		}
		if byUUID(id) != nil {
			writeJSON(w, 400, map[string]any{"message": "The user exists"})
			return
		}
		u := &user{ID: p.nextID, UUID: id, Name: str(b["name"]), Enabled: b["enable"] != false, Note: str(b["comment"]), Tg: int64(f64(b["telegram_id"]))}
		p.hidApply(u, b)
		p.nextID++
		p.users[u.UUID] = u
		writeJSON(w, 200, p.hidJSON(u))
	case strings.HasPrefix(path, "/user/"):
		id := strings.Trim(strings.TrimPrefix(path, "/user/"), "/")
		u := byUUID(id)
		if u == nil {
			writeJSON(w, 404, map[string]any{"message": "User not found"})
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, 200, p.hidJSON(u))
		case http.MethodPatch:
			b := decode(r)
			p.hidApply(u, b)
			if v, ok := b["enable"].(bool); ok {
				u.Enabled = v
			}
			if c, ok := b["comment"].(string); ok {
				u.Note = c
			}
			if _, ok := b["current_usage_GB"]; ok {
				u.Used = int64(f64(b["current_usage_GB"]) * float64(1<<30))
			}
			writeJSON(w, 200, p.hidJSON(u))
		case http.MethodDelete:
			delete(p.users, u.UUID)
			writeJSON(w, 200, map[string]any{"status": 200, "msg": "ok"})
		}
	default:
		writeJSON(w, 404, map[string]any{"message": "Not Found"})
	}
}

// hidApply stores usage limit and period; Expire holds start_date and
// OnHold the package length.
func (p *Panel) hidApply(u *user, b map[string]any) {
	if v, ok := b["usage_limit_GB"]; ok {
		u.Limit = int64(f64(v) * float64(1<<30))
	}
	if v, ok := b["package_days"]; ok {
		u.OnHold = time.Duration(f64(v)) * 24 * time.Hour
	}
	if v, ok := b["start_date"]; ok {
		if s, _ := v.(string); s != "" {
			u.Expire, _ = time.Parse("2006-01-02", s)
		} else {
			u.Expire = time.Time{}
		}
	}
}

func (p *Panel) hidJSON(u *user) map[string]any {
	var start, online any
	if !u.Expire.IsZero() {
		start = u.Expire.Format("2006-01-02")
	}
	if !u.Online.IsZero() {
		online = u.Online.UTC().Format("2006-01-02 15:04:05")
	}
	var tg any
	if u.Tg != 0 {
		tg = u.Tg
	}
	return map[string]any{
		"uuid": u.UUID, "name": u.Name, "usage_limit_GB": float64(u.Limit) / float64(1<<30), "package_days": int(u.OnHold.Hours() / 24),
		"mode": "no_reset", "start_date": start, "current_usage_GB": float64(u.Used) / float64(1<<30), "last_online": online,
		"comment": u.Note, "telegram_id": tg, "enable": u.Enabled, "is_active": u.Enabled,
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}
