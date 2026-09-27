// Package xuifake is an in-memory X-UI panel (3x-ui or alireza0 x-ui API)
// for tests and for the builder's test chat, which must never create users
// on a real panel.
package xuifake

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"

	"github.com/mrjvadi/tgcreator/internal/xui"
)

// Panel is a fake panel. Its URL includes a web base path, like real panels.
type Panel struct {
	URL      string
	Type     string
	Username string
	Password string

	srv      *httptest.Server
	api, set string
	base     string

	mu       sync.Mutex
	sessions map[string]bool
	inbounds []*xui.Inbound
	nextID   int
	onlines  []string
	Logins   int
	Requests []string // "METHOD path" of every API call
}

// New starts a fake panel of type "3x-ui" or "x-ui" with two inbounds:
// 1 = VLESS reality on 443, 2 = VMess websocket + TLS on 8443.
func New(panelType string) *Panel {
	p := &Panel{Type: panelType, Username: "admin", Password: "admin", base: "/secret", sessions: map[string]bool{}, nextID: 3}
	if panelType == "x-ui" {
		p.api, p.set = "/xui/API/inbounds", "/xui/setting"
	} else {
		p.Type, p.api, p.set = "3x-ui", "/panel/api/inbounds", "/panel/setting"
	}
	p.inbounds = []*xui.Inbound{
		{ID: 1, Remark: "Reality", Enable: true, Port: 443, Protocol: "vless",
			Settings:       `{"clients":[{"id":"11111111-1111-4111-8111-111111111111","flow":"xtls-rprx-vision","email":"seed","limitIp":0,"totalGB":0,"expiryTime":0,"enable":true,"tgId":0,"subId":"seedsub","reset":0}],"decryption":"none"}`,
			StreamSettings: `{"network":"tcp","security":"reality","realitySettings":{"serverNames":["www.speedtest.net"],"shortIds":["ab12"],"settings":{"publicKey":"PUBKEY","fingerprint":"chrome","spiderX":"/"}},"tcpSettings":{"header":{"type":"none"}}}`,
			ClientStats:    []xui.Traffic{{ID: 1, InboundID: 1, Enable: true, Email: "seed"}}},
		{ID: 2, Remark: "WS", Enable: true, Port: 8443, Protocol: "vmess",
			Settings:       `{"clients":[{"id":"22222222-2222-4222-8222-222222222222","security":"auto","email":"seed2","limitIp":0,"totalGB":0,"expiryTime":0,"enable":true,"tgId":0,"subId":"seed2sub","reset":0}]}`,
			StreamSettings: `{"network":"ws","security":"tls","wsSettings":{"path":"/ws","headers":{"Host":"cdn.example.com"}},"tlsSettings":{"serverName":"cdn.example.com","alpn":["h2","http/1.1"],"settings":{"fingerprint":"chrome"}}}`,
			ClientStats:    []xui.Traffic{{ID: 2, InboundID: 2, Enable: true, Email: "seed2"}}},
	}
	p.srv = httptest.NewServer(http.HandlerFunc(p.serve))
	p.URL = p.srv.URL + p.base + "/"
	return p
}

// Close stops the server.
func (p *Panel) Close() { p.srv.Close() }

// Addr is host:port of the server.
func (p *Panel) Addr() string { return strings.TrimPrefix(p.srv.URL, "http://") }

// ExpireSessions forgets every login, like a panel restart.
func (p *Panel) ExpireSessions() {
	p.mu.Lock()
	p.sessions = map[string]bool{}
	p.mu.Unlock()
}

// SetUsage sets a client's traffic counters.
func (p *Panel) SetUsage(email string, up, down int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, in := range p.inbounds {
		for i := range in.ClientStats {
			if in.ClientStats[i].Email == email {
				in.ClientStats[i].Up, in.ClientStats[i].Down = up, down
			}
		}
	}
}

// SetOnline sets the online client list.
func (p *Panel) SetOnline(emails ...string) {
	p.mu.Lock()
	p.onlines = emails
	p.mu.Unlock()
}

// Client returns the stored client with that email.
func (p *Panel) Client(email string) (map[string]any, *xui.Inbound) {
	p.mu.Lock()
	defer p.mu.Unlock()
	in, i := p.find(email)
	if in == nil {
		return nil, nil
	}
	cl, _ := in.Clients()
	return cl[i], in
}

func (p *Panel) find(email string) (*xui.Inbound, int) {
	for _, in := range p.inbounds {
		cl, _ := in.Clients()
		for i, c := range cl {
			if c["email"] == email {
				return in, i
			}
		}
	}
	return nil, -1
}

type reply struct {
	Success bool   `json:"success"`
	Msg     string `json:"msg"`
	Obj     any    `json:"obj"`
}

func (p *Panel) serve(w http.ResponseWriter, r *http.Request) {
	path, ok := strings.CutPrefix(r.URL.Path, p.base)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if path == "/login" && r.Method == http.MethodPost {
		p.login(w, r)
		return
	}
	cookie, _ := r.Cookie(p.cookie())
	p.mu.Lock()
	logged := cookie != nil && p.sessions[cookie.Value]
	p.mu.Unlock()
	if !logged {
		if p.Type == "3x-ui" && strings.HasPrefix(path, "/panel/api") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
			writeJSON(w, http.StatusUnauthorized, reply{Msg: "login again"})
			return
		}
		http.Redirect(w, r, p.base+"/", http.StatusTemporaryRedirect)
		return
	}
	p.mu.Lock()
	p.Requests = append(p.Requests, r.Method+" "+path)
	p.mu.Unlock()
	if path == p.set+"/defaultSettings" && r.Method == http.MethodPost {
		writeJSON(w, 200, reply{Success: true, Obj: map[string]any{"subEnable": true, "subURI": "https://sub.example.com:2096/sub/"}})
		return
	}
	rest, ok := strings.CutPrefix(path, p.api)
	if !ok {
		http.NotFound(w, r)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	obj, err := p.route(r, rest)
	if err != nil {
		writeJSON(w, 200, reply{Msg: "Something went wrong (" + err.Error() + ")"})
		return
	}
	writeJSON(w, 200, reply{Success: true, Obj: obj})
}

func (p *Panel) cookie() string {
	if p.Type == "x-ui" {
		return "x-ui"
	}
	return "3x-ui"
}

func (p *Panel) login(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.PostForm.Get("username") != p.Username || r.PostForm.Get("password") != p.Password {
		writeJSON(w, 200, reply{Msg: "Wrong username or password"})
		return
	}
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	sid := hex.EncodeToString(b)
	p.mu.Lock()
	p.sessions[sid] = true
	p.Logins++
	p.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: p.cookie(), Value: sid, Path: p.base + "/", HttpOnly: true})
	writeJSON(w, 200, reply{Success: true, Msg: "Login Successfully"})
}

func (p *Panel) route(r *http.Request, rest string) (any, error) {
	seg := strings.Split(strings.Trim(rest, "/"), "/")
	get := r.Method == http.MethodGet
	switch {
	case get && (rest == "/list" && p.Type == "3x-ui" || (rest == "/" || rest == "") && p.Type == "x-ui"):
		return p.inbounds, nil
	case get && seg[0] == "get" && len(seg) == 2:
		id, _ := strconv.Atoi(seg[1])
		for _, in := range p.inbounds {
			if in.ID == id {
				return in, nil
			}
		}
		return nil, fmt.Errorf("record not found")
	case get && seg[0] == "getClientTraffics" && len(seg) == 2:
		in, _ := p.find(seg[1])
		if in == nil {
			if p.Type == "3x-ui" {
				return nil, fmt.Errorf("Inbound Not Found For Email: %s", seg[1])
			}
			return nil, nil
		}
		st, _ := in.Stat(seg[1])
		return st, nil
	case !get && rest == "/addClient":
		var body struct {
			ID       int    `json:"id"`
			Settings string `json:"settings"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		in := p.inbound(body.ID)
		if in == nil {
			return nil, fmt.Errorf("record not found")
		}
		add := (&xui.Inbound{Settings: body.Settings})
		clients, err := add.Clients()
		if err != nil {
			return nil, err
		}
		for _, c := range clients {
			if e, _ := p.find(c["email"].(string)); e != nil {
				return nil, fmt.Errorf("Duplicate email: %s", c["email"])
			}
		}
		old, _ := in.Clients()
		p.setClients(in, append(old, clients...))
		for _, c := range clients {
			in.ClientStats = append(in.ClientStats, xui.Traffic{InboundID: in.ID, Email: c["email"].(string), Enable: c["enable"] == true,
				Total: int64(num(c["totalGB"])), ExpiryTime: int64(num(c["expiryTime"]))})
		}
		return nil, nil
	case !get && seg[0] == "updateClient" && len(seg) == 2:
		var body struct {
			ID       int    `json:"id"`
			Settings string `json:"settings"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		in := p.inbound(body.ID)
		if in == nil {
			return nil, fmt.Errorf("record not found")
		}
		upd, _ := (&xui.Inbound{Settings: body.Settings}).Clients()
		if len(upd) != 1 {
			return nil, fmt.Errorf("empty client ID")
		}
		key := xui.ClientKey(in.Protocol)
		old, _ := in.Clients()
		for i, c := range old {
			if c[key] == seg[1] {
				oldEmail := c["email"].(string)
				old[i] = upd[0]
				p.setClients(in, old)
				for j := range in.ClientStats {
					if in.ClientStats[j].Email == oldEmail {
						st := &in.ClientStats[j]
						st.Email, st.Enable = upd[0]["email"].(string), upd[0]["enable"] == true
						st.Total, st.ExpiryTime = int64(num(upd[0]["totalGB"])), int64(num(upd[0]["expiryTime"]))
					}
				}
				return nil, nil
			}
		}
		return nil, fmt.Errorf("empty client ID")
	case !get && len(seg) == 3 && seg[1] == "delClient":
		id, _ := strconv.Atoi(seg[0])
		in := p.inbound(id)
		if in == nil {
			return nil, fmt.Errorf("record not found")
		}
		key := xui.ClientKey(in.Protocol)
		old, _ := in.Clients()
		for i, c := range old {
			if c[key] == seg[2] {
				if len(old) == 1 {
					return nil, fmt.Errorf("no client remained in Inbound")
				}
				email := c["email"]
				p.setClients(in, append(old[:i:i], old[i+1:]...))
				for j := range in.ClientStats {
					if in.ClientStats[j].Email == email {
						in.ClientStats = append(in.ClientStats[:j:j], in.ClientStats[j+1:]...)
						break
					}
				}
				return nil, nil
			}
		}
		return nil, fmt.Errorf("Client Not Found In Inbound For ID: %s", seg[2])
	case !get && len(seg) == 3 && seg[1] == "resetClientTraffic":
		id, _ := strconv.Atoi(seg[0])
		if in := p.inbound(id); in != nil {
			for j := range in.ClientStats {
				if in.ClientStats[j].Email == seg[2] {
					in.ClientStats[j].Up, in.ClientStats[j].Down = 0, 0
					in.ClientStats[j].Enable = true
				}
			}
		}
		return nil, nil
	case !get && rest == "/onlines":
		return p.onlines, nil
	}
	return nil, fmt.Errorf("fake panel: unsupported %s %s", r.Method, rest)
}

func (p *Panel) inbound(id int) *xui.Inbound {
	for _, in := range p.inbounds {
		if in.ID == id {
			return in
		}
	}
	return nil
}

func (p *Panel) setClients(in *xui.Inbound, clients []map[string]any) {
	var s map[string]any
	_ = json.Unmarshal([]byte(in.Settings), &s)
	s["clients"] = clients
	raw, _ := json.Marshal(s)
	in.Settings = string(raw)
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
