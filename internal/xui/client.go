// Package xui talks to X-UI panels: MHSanaei's 3x-ui and alireza0's x-ui.
//
// Both keep a cookie session after POST /login and answer every API call
// with {success, msg, obj}. The client logs in lazily, shares one session
// between all goroutines and logs in again when the panel forgets it
// (restart, session expiry).
package xui

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// ErrNotFound means the panel has no client with that email.
var ErrNotFound = errors.New("client not found")

// ErrExists means a client with that email already exists.
var ErrExists = errors.New("client email already exists")

// Error is a failure the panel reported in its {success:false, msg} reply.
type Error struct {
	Path string
	Msg  string
}

func (e *Error) Error() string { return "x-ui " + e.Path + ": " + e.Msg }

type flavor struct {
	api     string // inbounds API group
	list    string // list route inside it
	setting string // settings group (UI routes)
}

var flavors = map[string]flavor{
	"3x-ui": {api: "/panel/api/inbounds", list: "/list", setting: "/panel/setting"},
	"x-ui":  {api: "/xui/API/inbounds", list: "/", setting: "/xui/setting"},
}

// Client is safe for concurrent use.
type Client struct {
	cfg  workflow.VPNPanel
	base string // panel URL without trailing slash
	fl   flavor
	hc   *http.Client

	mu    sync.Mutex // serializes logins
	state sync.RWMutex
	gen   uint64 // bumped on every successful login
	valid bool

	subMu   sync.Mutex
	subBase string
	subAt   time.Time
}

// New validates the configuration; it does not contact the panel.
func New(cfg workflow.VPNPanel) (*Client, error) {
	if cfg.Type == "" {
		cfg.Type = "3x-ui"
	}
	fl, ok := flavors[cfg.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported panel type %q (3x-ui, x-ui)", cfg.Type)
	}
	if cfg.APIPath != "" {
		fl.api = "/" + strings.Trim(cfg.APIPath, "/")
	}
	u, err := url.Parse(strings.TrimSpace(cfg.URL))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("panel url %q is not an http(s) address", cfg.URL)
	}
	if cfg.Username == "" {
		return nil, errors.New("panel username is empty")
	}
	timeout := 15 * time.Second
	if cfg.Timeout != "" {
		if timeout, err = time.ParseDuration(cfg.Timeout); err != nil {
			return nil, fmt.Errorf("timeout: %w", err)
		}
	}
	jar, _ := cookiejar.New(nil)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConnsPerHost = 32
	if cfg.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in for self-signed panels
	}
	return &Client{
		cfg:  cfg,
		base: strings.TrimRight(u.String(), "/"),
		fl:   fl,
		hc: &http.Client{
			Jar:       jar,
			Transport: tr,
			Timeout:   timeout,
			// A redirect means "not logged in"; never follow it to the login page.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// Host is the panel's host name, the default address in config links.
func (c *Client) Host() string {
	u, _ := url.Parse(c.base)
	return u.Hostname()
}

type envelope struct {
	Success bool            `json:"success"`
	Msg     string          `json:"msg"`
	Obj     json.RawMessage `json:"obj"`
}

// Login opens a new session. Calls log in on their own when needed.
func (c *Client) Login(ctx context.Context) error {
	c.state.RLock()
	gen := c.gen
	c.state.RUnlock()
	return c.relogin(ctx, gen, true)
}

// relogin logs in unless another goroutine already did after session gen.
func (c *Client) relogin(ctx context.Context, gen uint64, force bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.RLock()
	done := c.valid && (c.gen != gen || !force)
	c.state.RUnlock()
	if done {
		return nil
	}
	form := url.Values{"username": {c.cfg.Username}, "password": {c.cfg.Password}}
	if c.cfg.TOTPSecret != "" {
		code, err := TOTP(c.cfg.TOTPSecret, time.Now())
		if err != nil {
			return fmt.Errorf("totp: %w", err)
		}
		form.Set("twoFactorCode", code)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("x-ui login: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var env envelope
	if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &env) != nil {
		return fmt.Errorf("x-ui login: unexpected reply %s from %s/login (check the panel url and its base path)", resp.Status, c.base)
	}
	if !env.Success {
		return fmt.Errorf("x-ui login failed: %s", env.Msg)
	}
	c.state.Lock()
	c.gen++
	c.valid = true
	c.state.Unlock()
	return nil
}

// Call sends an API request to path (relative to the panel URL) and returns
// the reply's obj. body is sent as JSON when not nil.
func (c *Client) Call(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	retried := false
	for {
		c.state.RLock()
		gen, valid := c.gen, c.valid
		c.state.RUnlock()
		if !valid {
			if err := c.relogin(ctx, gen, false); err != nil {
				return nil, err
			}
			continue
		}
		status, raw, err := c.send(ctx, method, path, payload)
		if err != nil {
			return nil, err
		}
		var env envelope
		jsonErr := json.Unmarshal(raw, &env)
		// 3x-ui hides its API behind 404 without a session, x-ui answers 401
		// or redirects to the login page.
		if status == http.StatusUnauthorized || status == http.StatusNotFound || (status >= 300 && status < 400) || (status == http.StatusOK && jsonErr != nil) {
			if !retried {
				retried = true
				if err := c.relogin(ctx, gen, true); err != nil {
					return nil, err
				}
				continue
			}
			if status == http.StatusNotFound {
				return nil, fmt.Errorf("x-ui %s: not found (is the panel type %q right?)", path, c.cfg.Type)
			}
			return nil, fmt.Errorf("x-ui %s: http %d", path, status)
		}
		if status != http.StatusOK || jsonErr != nil {
			return nil, fmt.Errorf("x-ui %s: http %d: %s", path, status, truncate(string(raw), 200))
		}
		if !env.Success {
			return nil, &Error{Path: path, Msg: env.Msg}
		}
		return env.Obj, nil
	}
}

func (c *Client) send(ctx context.Context, method, path string, payload []byte) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return 0, nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("x-ui %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	return resp.StatusCode, raw, err
}

// Inbound is an inbound as the panel returns it. Settings and
// StreamSettings are JSON documents encoded as strings.
type Inbound struct {
	ID             int       `json:"id"`
	Remark         string    `json:"remark"`
	Enable         bool      `json:"enable"`
	Listen         string    `json:"listen"`
	Port           int       `json:"port"`
	Protocol       string    `json:"protocol"`
	Up             int64     `json:"up"`
	Down           int64     `json:"down"`
	Total          int64     `json:"total"`
	ExpiryTime     int64     `json:"expiryTime"`
	Settings       string    `json:"settings"`
	StreamSettings string    `json:"streamSettings"`
	ClientStats    []Traffic `json:"clientStats"`
}

// Traffic is a client's usage record.
type Traffic struct {
	ID         int    `json:"id"`
	InboundID  int    `json:"inboundId"`
	Enable     bool   `json:"enable"`
	Email      string `json:"email"`
	Up         int64  `json:"up"`
	Down       int64  `json:"down"`
	Total      int64  `json:"total"`
	ExpiryTime int64  `json:"expiryTime"`
	LastOnline int64  `json:"lastOnline"`
}

// Clients decodes the inbound's client list; entries keep every field the
// panel stores so updates never drop unknown ones.
func (in *Inbound) Clients() ([]map[string]any, error) {
	var s struct {
		Clients []map[string]any `json:"clients"`
	}
	if in.Settings == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(in.Settings), &s); err != nil {
		return nil, fmt.Errorf("inbound %d settings: %w", in.ID, err)
	}
	return s.Clients, nil
}

// Stat returns the traffic record of email, if the inbound carries it.
func (in *Inbound) Stat(email string) (Traffic, bool) {
	for _, t := range in.ClientStats {
		if t.Email == email {
			return t, true
		}
	}
	return Traffic{}, false
}

// ClientKey is the field that identifies clients of a protocol in the
// updateClient/delClient routes.
func ClientKey(protocol string) string {
	switch protocol {
	case "trojan":
		return "password"
	case "shadowsocks":
		return "email"
	case "hysteria", "hysteria2":
		return "auth"
	}
	return "id"
}

// Inbounds lists all inbounds with their clients.
func (c *Client) Inbounds(ctx context.Context) ([]Inbound, error) {
	raw, err := c.Call(ctx, http.MethodGet, c.fl.api+c.fl.list, nil)
	if err != nil {
		return nil, err
	}
	var out []Inbound
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("x-ui inbounds: %w", err)
	}
	return out, nil
}

// Inbound fetches one inbound.
func (c *Client) Inbound(ctx context.Context, id int) (*Inbound, error) {
	raw, err := c.Call(ctx, http.MethodGet, c.fl.api+"/get/"+strconv.Itoa(id), nil)
	if err != nil {
		return nil, err
	}
	var in Inbound
	if err := json.Unmarshal(raw, &in); err != nil || in.ID == 0 {
		return nil, fmt.Errorf("x-ui: inbound %d not found", id)
	}
	return &in, nil
}

// Found is a client together with the inbound that holds it.
type Found struct {
	Inbound *Inbound
	Client  map[string]any
	Traffic Traffic
}

// FindClient looks a client up by email. It returns ErrNotFound when the
// panel has no such client.
func (c *Client) FindClient(ctx context.Context, email string) (*Found, error) {
	if email == "" {
		return nil, ErrNotFound
	}
	raw, err := c.Call(ctx, http.MethodGet, c.fl.api+"/getClientTraffics/"+url.PathEscape(email), nil)
	if err != nil {
		var pe *Error
		if errors.As(err, &pe) && strings.Contains(strings.ToLower(pe.Msg), "not found") {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var t *Traffic
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &t) != nil || t == nil || t.InboundID == 0 {
		return nil, ErrNotFound
	}
	in, err := c.Inbound(ctx, t.InboundID)
	if err != nil {
		return nil, err
	}
	clients, err := in.Clients()
	if err != nil {
		return nil, err
	}
	for _, cl := range clients {
		if s, _ := cl["email"].(string); s == email {
			if st, ok := in.Stat(email); ok {
				*t = st
			}
			return &Found{Inbound: in, Client: cl, Traffic: *t}, nil
		}
	}
	return nil, ErrNotFound
}

// AddClient adds client to an inbound. Fields the protocol needs (id,
// password...) must already be set; see NewClient.
func (c *Client) AddClient(ctx context.Context, inboundID int, client map[string]any) error {
	settings, _ := json.Marshal(map[string]any{"clients": []any{client}})
	_, err := c.Call(ctx, http.MethodPost, c.fl.api+"/addClient", map[string]any{"id": inboundID, "settings": string(settings)})
	var pe *Error
	if errors.As(err, &pe) && strings.Contains(strings.ToLower(pe.Msg), "duplicate email") {
		return ErrExists
	}
	return err
}

// UpdateClient replaces the stored client f.Client with client.
func (c *Client) UpdateClient(ctx context.Context, f *Found, client map[string]any) error {
	key := ClientKey(f.Inbound.Protocol)
	id, _ := f.Client[key].(string)
	settings, _ := json.Marshal(map[string]any{"clients": []any{client}})
	_, err := c.Call(ctx, http.MethodPost, c.fl.api+"/updateClient/"+url.PathEscape(id), map[string]any{"id": f.Inbound.ID, "settings": string(settings)})
	return err
}

// DeleteClient removes a client from its inbound.
func (c *Client) DeleteClient(ctx context.Context, f *Found) error {
	id, _ := f.Client[ClientKey(f.Inbound.Protocol)].(string)
	_, err := c.Call(ctx, http.MethodPost, fmt.Sprintf("%s/%d/delClient/%s", c.fl.api, f.Inbound.ID, url.PathEscape(id)), nil)
	return err
}

// ResetTraffic zeroes a client's usage.
func (c *Client) ResetTraffic(ctx context.Context, inboundID int, email string) error {
	_, err := c.Call(ctx, http.MethodPost, fmt.Sprintf("%s/%d/resetClientTraffic/%s", c.fl.api, inboundID, url.PathEscape(email)), nil)
	return err
}

// Onlines lists the emails of connected clients.
func (c *Client) Onlines(ctx context.Context) ([]string, error) {
	raw, err := c.Call(ctx, http.MethodPost, c.fl.api+"/onlines", nil)
	if err != nil {
		return nil, err
	}
	var out []string
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SubBase is the subscription URL prefix (".../sub/"), from the config or
// else from the panel's own settings. Empty when subscriptions are off.
func (c *Client) SubBase(ctx context.Context) string {
	if c.cfg.SubURL != "" {
		return c.cfg.SubURL
	}
	c.subMu.Lock()
	defer c.subMu.Unlock()
	if !c.subAt.IsZero() && time.Since(c.subAt) < 10*time.Minute {
		return c.subBase
	}
	raw, err := c.Call(ctx, http.MethodPost, c.fl.setting+"/defaultSettings", nil)
	if err != nil {
		// Keep the last known value and retry in a minute, not on every call.
		c.subAt = time.Now().Add(-9 * time.Minute)
		return c.subBase
	}
	var s struct {
		SubEnable bool   `json:"subEnable"`
		SubURI    string `json:"subURI"`
	}
	_ = json.Unmarshal(raw, &s)
	c.subBase, c.subAt = "", time.Now()
	if s.SubEnable {
		c.subBase = s.SubURI
	}
	return c.subBase
}

// SubLink joins the subscription base and a client's subId.
func SubLink(base, subID string) string {
	if base == "" || subID == "" {
		return ""
	}
	if strings.HasSuffix(base, "/") {
		return base + subID
	}
	return base + "/" + subID
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Type is the panel flavour, "3x-ui" or "x-ui".
func (c *Client) Type() string { return c.cfg.Type }
