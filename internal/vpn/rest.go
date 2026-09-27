package vpn

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// HTTPError is a non-2xx reply.
type HTTPError struct {
	Status int
	Path   string
	Msg    string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("%s: http %d: %s", e.Path, e.Status, e.Msg) }

func isStatus(err error, codes ...int) bool {
	var he *HTTPError
	if !errors.As(err, &he) {
		return false
	}
	for _, c := range codes {
		if he.Status == c {
			return true
		}
	}
	return false
}

// rest is a JSON API client shared by the token-based panels. It logs in
// lazily, shares the token between goroutines and logs in again on 401.
type rest struct {
	base    string // API root, no trailing slash
	origin  string // scheme://host of the panel
	hc      *http.Client
	headers map[string]string
	// login returns a bearer token; nil means static headers are the auth.
	login func(ctx context.Context) (string, error)

	mu    sync.Mutex // serializes logins
	state sync.RWMutex
	token string
	gen   uint64
}

func newRest(cfg workflow.VPNPanel, apiPath string) (*rest, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.URL))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("panel url %q is not an http(s) address", cfg.URL)
	}
	timeout := 15 * time.Second
	if cfg.Timeout != "" {
		if timeout, err = time.ParseDuration(cfg.Timeout); err != nil {
			return nil, fmt.Errorf("timeout: %w", err)
		}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConnsPerHost = 32
	if cfg.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in for self-signed panels
	}
	return &rest{
		base:    strings.TrimRight(u.String(), "/") + apiPath,
		origin:  u.Scheme + "://" + u.Host,
		hc:      &http.Client{Transport: tr, Timeout: timeout},
		headers: map[string]string{},
	}, nil
}

// formLogin is the OAuth2 password flow of the FastAPI panels.
func (r *rest) formLogin(path, user, pass string) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		form := url.Values{"username": {user}, "password": {pass}, "grant_type": {"password"}}
		var tok struct {
			AccessToken string `json:"access_token"`
		}
		if err := r.send(ctx, http.MethodPost, path, "", form, nil, &tok); err != nil {
			if isStatus(err, 401, 403) {
				return "", fmt.Errorf("login failed: wrong username or password")
			}
			return "", fmt.Errorf("login: %w", err)
		}
		if tok.AccessToken == "" {
			return "", errors.New("login: the panel returned no token")
		}
		return tok.AccessToken, nil
	}
}

func (r *rest) ensureToken(ctx context.Context, stale uint64, force bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.RLock()
	done := r.token != "" && (!force || r.gen != stale)
	r.state.RUnlock()
	if done {
		return nil
	}
	tok, err := r.login(ctx)
	if err != nil {
		return err
	}
	r.state.Lock()
	r.token, r.gen = tok, r.gen+1
	r.state.Unlock()
	return nil
}

// do sends a JSON request (body may be nil) and decodes the reply into out.
func (r *rest) do(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	retried := false
	for {
		token, gen := "", uint64(0)
		if r.login != nil {
			r.state.RLock()
			token, gen = r.token, r.gen
			r.state.RUnlock()
			if token == "" {
				if err := r.ensureToken(ctx, gen, false); err != nil {
					return err
				}
				continue
			}
		}
		err := r.send(ctx, method, path, token, nil, payload, out)
		if r.login != nil && !retried && isStatus(err, http.StatusUnauthorized) {
			retried = true
			if err := r.ensureToken(ctx, gen, true); err != nil {
				return err
			}
			continue
		}
		return err
	}
}

func (r *rest) send(ctx context.Context, method, path, token string, form url.Values, payload []byte, out any) error {
	var body io.Reader
	ctype := ""
	switch {
	case form != nil:
		body, ctype = strings.NewReader(form.Encode()), "application/x-www-form-urlencoded"
	case payload != nil:
		body, ctype = bytes.NewReader(payload), "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, body)
	if err != nil {
		return err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := r.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &HTTPError{Status: resp.StatusCode, Path: path, Msg: errorText(raw)}
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if rm, ok := out.(*json.RawMessage); ok {
		*rm = append((*rm)[:0], raw...)
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: unexpected reply: %w (%s)", path, err, truncate(string(raw), 120))
	}
	return nil
}

// errorText pulls the message out of FastAPI ({detail}), NestJS
// ({message}) and Flask ({message|msg}) error bodies.
func errorText(raw []byte) string {
	var e struct {
		Detail  any    `json:"detail"`
		Message any    `json:"message"`
		Msg     string `json:"msg"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil {
		for _, v := range []any{e.Detail, e.Message, e.Msg, e.Error} {
			switch t := v.(type) {
			case string:
				if t != "" {
					return t
				}
			case nil:
			default:
				b, _ := json.Marshal(t)
				return truncate(string(b), 300)
			}
		}
	}
	return truncate(strings.TrimSpace(string(raw)), 300)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// absURL makes a subscription path returned by the panel absolute.
func absURL(link, origin, subBase string) string {
	if link == "" || strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
		return link
	}
	if subBase != "" {
		if u, err := url.Parse(subBase); err == nil && u.Host != "" {
			origin = u.Scheme + "://" + u.Host
		}
	}
	if !strings.HasPrefix(link, "/") {
		// A bare domain ("sub.example.com/...") from panels that store it that way.
		if i := strings.IndexByte(link, '/'); i > 0 && strings.Contains(link[:i], ".") {
			return "https://" + link
		}
		link = "/" + link
	}
	return origin + link
}

// parseTime reads the date formats the panels return: unix seconds,
// RFC 3339 and naive ISO timestamps (UTC).
func parseTime(v any) time.Time {
	switch t := v.(type) {
	case float64:
		if t <= 0 {
			return time.Time{}
		}
		return time.Unix(int64(t), 0)
	case string:
		if t == "" {
			return time.Time{}
		}
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999", "2006-01-02"} {
			if tm, err := time.Parse(layout, t); err == nil {
				return tm
			}
		}
	}
	return time.Time{}
}

func num(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case json.Number:
		f, _ := t.Float64()
		return f
	case int64:
		return float64(t)
	case int:
		return float64(t)
	}
	return 0
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	case float64:
		return fmt.Sprintf("%.0f", t)
	}
	return fmt.Sprint(v)
}
