package vpn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// marzban adapts Marzban (Gozargah) and its successor PasarGuard, which
// share the API shape: OAuth2 token at /api/admin/token, users at
// /api/user/{username} and /api/users. They differ in how users attach
// to inbounds (Marzban: proxies + inbound tags, PasarGuard: groups) and
// in the expire field (unix seconds vs. ISO date).
type marzban struct {
	r      *rest
	cfg    workflow.VPNPanel
	pasarg bool
}

// panelRoot drops a dashboard path users often paste with the address.
func panelRoot(cfg workflow.VPNPanel, cut ...string) workflow.VPNPanel {
	u, err := url.Parse(strings.TrimSpace(cfg.URL))
	if err != nil {
		return cfg
	}
	for _, c := range cut {
		if i := strings.Index(u.Path, c); i >= 0 {
			u.Path = u.Path[:i]
		}
	}
	u.RawQuery, u.Fragment = "", ""
	cfg.URL = u.String()
	return cfg
}

func newMarzban(cfg workflow.VPNPanel) (Panel, error) {
	cfg = panelRoot(cfg, "/dashboard")
	r, err := newRest(cfg, "/api")
	if err != nil {
		return nil, err
	}
	if cfg.Username == "" {
		return nil, errors.New("panel username is empty")
	}
	r.login = r.formLogin("/admin/token", cfg.Username, cfg.Password)
	return &marzban{r: r, cfg: cfg, pasarg: cfg.Type == "pasarguard"}, nil
}

func (m *marzban) Type() string {
	if m.pasarg {
		return "pasarguard"
	}
	return "marzban"
}

func (m *marzban) userPath(u string) string { return "/user/" + url.PathEscape(u) }

func (m *marzban) account(u map[string]any) *Account {
	a := &Account{
		Username: str(u["username"]),
		Status:   str(u["status"]),
		Used:     int64(num(u["used_traffic"])),
		Total:    int64(num(u["data_limit"])),
		OnlineAt: parseTime(u["online_at"]),
		SubLink:  absURL(str(u["subscription_url"]), m.r.origin, m.cfg.SubURL),
		LimitIP:  int(num(u["hwid_limit"])),
	}
	a.Enabled = a.Status != "disabled"
	note := str(u["note"])
	a.TgID = tgFromNote(note)
	a.Note = strings.TrimSpace(tgMark.ReplaceAllString(note, ""))
	if d := num(u["on_hold_expire_duration"]); a.Status == "on_hold" && d > 0 {
		a.Expire = Expiry{Kind: Pending, Duration: time.Duration(d) * time.Second}
	} else if t := parseTime(u["expire"]); !t.IsZero() {
		a.Expire = Expiry{Kind: At, At: t}
	}
	proxies, _ := u["proxies"].(map[string]any)
	if m.pasarg {
		proxies, _ = u["proxy_settings"].(map[string]any)
		for _, g := range asList(u["group_ids"]) {
			a.Groups = append(a.Groups, str(g))
		}
	} else {
		ins, _ := u["inbounds"].(map[string]any)
		for _, proto := range sortedKeys(ins) {
			for _, tag := range asList(ins[proto]) {
				a.Groups = append(a.Groups, str(tag))
			}
		}
	}
	for _, proto := range []string{"vless", "vmess", "trojan", "shadowsocks"} {
		if p, ok := proxies[proto].(map[string]any); ok {
			a.Protocol = proto
			a.ID = str(p["id"])
			if a.ID == "" {
				a.ID = str(p["password"])
			}
			break
		}
	}
	for _, l := range asList(u["links"]) {
		a.Links = append(a.Links, str(l))
	}
	return a
}

func (m *marzban) Get(ctx context.Context, username string) (*Account, error) {
	var u map[string]any
	if err := m.r.do(ctx, http.MethodGet, m.userPath(username), nil, &u); err != nil {
		if isStatus(err, http.StatusNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return m.account(u), nil
}

// expireValue is how the panel wants a fixed date: unix seconds (both
// accept an integer; 0 means never).
func expireValue(e Expiry) int64 {
	if e.Kind == At {
		return e.At.Unix()
	}
	return 0
}

func (m *marzban) Create(ctx context.Context, s Spec) (*Account, error) {
	exp := ExpiryFor(s.Days, s.AfterFirstUse, time.Now())
	body := map[string]any{
		"username":                  s.Username,
		"data_limit":                s.TotalBytes,
		"data_limit_reset_strategy": "no_reset",
		"expire":                    expireValue(exp),
		"note":                      noteWithTg(s.Note, s.TgID),
		"status":                    "active",
	}
	if exp.Kind == Pending {
		body["status"], body["on_hold_expire_duration"] = "on_hold", int64(exp.Duration.Seconds())
	}
	if m.pasarg {
		groups, err := m.groupIDs(ctx, s.Groups)
		if err != nil {
			return nil, err
		}
		body["group_ids"] = groups
		if s.LimitIP > 0 {
			body["hwid_limit"] = s.LimitIP
		}
	} else {
		proxies, inbounds, err := m.proxies(ctx, s.Groups, s.Flow)
		if err != nil {
			return nil, err
		}
		body["proxies"] = proxies
		if inbounds != nil {
			body["inbounds"] = inbounds
		}
	}
	path := "/user"
	var u map[string]any
	if err := m.r.do(ctx, http.MethodPost, path, body, &u); err != nil {
		if isStatus(err, http.StatusConflict) {
			return nil, ErrExists
		}
		return nil, err
	}
	a := m.account(u)
	if s.Disabled {
		// Both panels create only active or on-hold users.
		return m.Update(ctx, s.Username, Change{Enable: ptr(false)})
	}
	return a, nil
}

// proxies picks Marzban protocols: those of the chosen inbound tags, or
// every protocol the server has inbounds for.
func (m *marzban) proxies(ctx context.Context, tags []string, flow string) (map[string]any, map[string][]string, error) {
	var all map[string][]struct {
		Tag string `json:"tag"`
	}
	if err := m.r.do(ctx, http.MethodGet, "/inbounds", nil, &all); err != nil {
		return nil, nil, err
	}
	want := map[string]bool{}
	for _, t := range tags {
		if t = strings.TrimSpace(t); t != "" {
			want[t] = true
		}
	}
	proxies := map[string]any{}
	var inbounds map[string][]string
	if len(want) > 0 {
		inbounds = map[string][]string{}
	}
	for proto, list := range all {
		for _, in := range list {
			if len(want) == 0 || want[in.Tag] {
				settings := map[string]any{}
				if proto == "vless" && flow != "" {
					settings["flow"] = flow
				}
				proxies[proto] = settings
				if inbounds != nil {
					inbounds[proto] = append(inbounds[proto], in.Tag)
					delete(want, in.Tag)
				}
			}
		}
	}
	if len(want) > 0 {
		return nil, nil, fmt.Errorf("marzban: inbound %q not found", sortedKeys(want)[0])
	}
	if len(proxies) == 0 {
		return nil, nil, errors.New("marzban: the server has no inbounds")
	}
	return proxies, inbounds, nil
}

// groupIDs parses PasarGuard group ids; empty means every enabled group.
func (m *marzban) groupIDs(ctx context.Context, ids []string) ([]int, error) {
	var out []int
	for _, s := range ids {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return nil, fmt.Errorf("pasarguard: group id %q is not a number", s)
		}
		out = append(out, n)
	}
	if len(out) > 0 {
		return out, nil
	}
	groups, err := m.Groups(ctx)
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		if g.Enabled {
			n, _ := strconv.Atoi(g.ID)
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("pasarguard: create a group first (users must belong to one)")
	}
	return out, nil
}

func (m *marzban) Update(ctx context.Context, username string, ch Change) (*Account, error) {
	a, err := m.Get(ctx, username)
	if err != nil {
		return nil, err
	}
	old := *a
	renewed := ch.Apply(a, time.Now())
	body := map[string]any{}
	if renewed {
		// Sent together so the panel re-evaluates expired/limited status.
		body["data_limit"] = a.Total
		switch a.Expire.Kind {
		case Pending:
			body["on_hold_expire_duration"] = int64(a.Expire.Duration.Seconds())
		default:
			body["expire"] = expireValue(a.Expire)
			if old.Expire.Kind == Pending {
				body["status"] = "active"
			}
		}
	}
	if ch.Enable != nil {
		if *ch.Enable {
			if old.Expire.Kind == Pending && a.Expire.Kind == Pending {
				body["status"] = "on_hold"
			} else {
				body["status"] = "active"
			}
		} else {
			body["status"] = "disabled"
		}
	}
	if ch.Note != nil {
		body["note"] = noteWithTg(a.Note, a.TgID)
	}
	if ch.LimitIP != nil && m.pasarg {
		body["hwid_limit"] = *ch.LimitIP
	}
	if len(body) > 0 {
		if err := m.r.do(ctx, http.MethodPut, m.userPath(username), body, nil); err != nil {
			return nil, err
		}
	}
	if ch.ResetUsage {
		if err := m.r.do(ctx, http.MethodPost, m.userPath(username)+"/reset", nil, nil); err != nil {
			return nil, err
		}
	}
	return m.Get(ctx, username)
}

func (m *marzban) Delete(ctx context.Context, username string) error {
	err := m.r.do(ctx, http.MethodDelete, m.userPath(username), nil, nil)
	if isStatus(err, http.StatusNotFound) {
		return ErrNotFound
	}
	return err
}

func (m *marzban) list(ctx context.Context, search string) ([]*Account, error) {
	const page = 500
	var out []*Account
	for offset := 0; ; offset += page {
		q := url.Values{"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(page)}}
		if search != "" {
			q.Set("search", search)
		}
		var res struct {
			Users []map[string]any `json:"users"`
			Total int              `json:"total"`
		}
		if err := m.r.do(ctx, http.MethodGet, "/users?"+q.Encode(), nil, &res); err != nil {
			return nil, err
		}
		for _, u := range res.Users {
			out = append(out, m.account(u))
		}
		if len(res.Users) < page || len(out) >= res.Total {
			return out, nil
		}
	}
}

func (m *marzban) Find(ctx context.Context, q Query) ([]*Account, error) {
	// The server searches usernames and notes, where "tg:<id>" is kept.
	search := q.Search
	if search == "" && q.TgID != "" {
		search = "tg:" + q.TgID
	}
	return filter(m.list(ctx, search))(q)
}

func (m *marzban) Onlines(ctx context.Context) ([]string, error) {
	return onlinesOf(m.list(ctx, ""))
}

func (m *marzban) Groups(ctx context.Context) ([]Group, error) {
	if m.pasarg {
		var res struct {
			Groups []struct {
				ID         int    `json:"id"`
				Name       string `json:"name"`
				IsDisabled bool   `json:"is_disabled"`
				TotalUsers int    `json:"total_users"`
			} `json:"groups"`
		}
		if err := m.r.do(ctx, http.MethodGet, "/groups", nil, &res); err != nil {
			return nil, err
		}
		out := make([]Group, len(res.Groups))
		for i, g := range res.Groups {
			out[i] = Group{ID: strconv.Itoa(g.ID), Name: g.Name, Users: g.TotalUsers, Enabled: !g.IsDisabled}
		}
		return out, nil
	}
	var all map[string][]struct {
		Tag     string `json:"tag"`
		Network string `json:"network"`
		Port    any    `json:"port"`
	}
	if err := m.r.do(ctx, http.MethodGet, "/inbounds", nil, &all); err != nil {
		return nil, err
	}
	var out []Group
	for _, proto := range sortedKeys(all) {
		for _, in := range all[proto] {
			port, _ := strconv.Atoi(str(in.Port))
			out = append(out, Group{ID: in.Tag, Name: in.Tag, Protocol: proto, Port: port, Enabled: true})
		}
	}
	return out, nil
}

func (m *marzban) Call(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var raw json.RawMessage
	err := m.r.do(ctx, method, "/"+strings.TrimPrefix(strings.TrimPrefix(path, "/"), "api/"), body, &raw)
	return raw, err
}

// filter applies a query to a listing.
func filter(list []*Account, err error) func(Query) ([]*Account, error) {
	return func(q Query) ([]*Account, error) {
		if err != nil {
			return nil, err
		}
		out := list[:0]
		for _, a := range list {
			if q.Match(a) {
				out = append(out, a)
			}
		}
		return out, nil
	}
}

// onlinesOf lists users seen within OnlineWindow.
func onlinesOf(list []*Account, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := []string{}
	for _, a := range list {
		if !a.OnlineAt.IsZero() && now.Sub(a.OnlineAt) < OnlineWindow {
			out = append(out, a.Username)
		}
	}
	return out, nil
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func ptr[T any](v T) *T { return &v }

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
