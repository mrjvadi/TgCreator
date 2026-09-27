package vpn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// remnawave adapts Remnawave (API contract @remnawave/backend-contract
// 3.x): bearer API token (or /api/auth/login), replies wrapped in
// {"response": ...}, users updated with PATCH /api/users and addressed by
// id in action routes, internal squads instead of inbounds. It has no
// "starts on first use" expiry and no "never": a far date stands for it.
type remnawave struct {
	r   *rest
	cfg workflow.VPNPanel
}

// remnaNever is the expireAt used for unlimited users.
var remnaNever = time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC)

func newRemnawave(cfg workflow.VPNPanel) (Panel, error) {
	r, err := newRest(panelRoot(cfg, "/dashboard", "/auth"), "/api")
	if err != nil {
		return nil, err
	}
	// Remnawave refuses plain-HTTP API calls unless a proxy vouches for them.
	r.headers["X-Forwarded-Proto"] = "https"
	r.headers["X-Forwarded-For"] = "127.0.0.1"
	rw := &remnawave{r: r, cfg: cfg}
	switch {
	case cfg.Token != "":
		r.headers["Authorization"] = "Bearer " + cfg.Token
	case cfg.Username != "":
		r.login = func(ctx context.Context) (string, error) {
			var res struct {
				Response struct {
					AccessToken string `json:"accessToken"`
				} `json:"response"`
			}
			if err := r.send(ctx, http.MethodPost, "/auth/login", "", nil, mustJSON(map[string]string{"username": cfg.Username, "password": cfg.Password}), &res); err != nil {
				return "", fmt.Errorf("login: %w", err)
			}
			if res.Response.AccessToken == "" {
				return "", errors.New("login: the panel returned no token")
			}
			return res.Response.AccessToken, nil
		}
	default:
		return nil, errors.New("remnawave: set an API token (Settings → API tokens)")
	}
	return rw, nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func (p *remnawave) Type() string { return "remnawave" }

func (p *remnawave) account(u map[string]any) *Account {
	tr, _ := u["userTraffic"].(map[string]any)
	if tr == nil {
		tr = u // before 2.x the traffic fields were on the user
	}
	a := &Account{
		Username: str(u["username"]),
		ID:       str(u["vlessUuid"]),
		Status:   strings.ToLower(str(u["status"])),
		Used:     int64(num(tr["usedTrafficBytes"])),
		Total:    int64(num(u["trafficLimitBytes"])),
		OnlineAt: parseTime(tr["onlineAt"]),
		SubLink:  absURL(str(u["subscriptionUrl"]), p.r.origin, p.cfg.SubURL),
		Note:     str(u["description"]),
		TgID:     str(u["telegramId"]),
		LimitIP:  int(num(u["hwidDeviceLimit"])),
		ref:      str(u["uuid"]),
	}
	if a.ref == "" {
		a.ref = str(u["id"])
	}
	a.Enabled = a.Status != "disabled"
	if t := parseTime(u["expireAt"]); !t.IsZero() && t.Before(remnaNever.AddDate(-1, 0, 0)) {
		a.Expire = Expiry{Kind: At, At: t}
	}
	for _, sq := range asList(u["activeInternalSquads"]) {
		if m, ok := sq.(map[string]any); ok {
			a.Groups = append(a.Groups, str(m["uuid"]))
		} else {
			a.Groups = append(a.Groups, str(sq))
		}
	}
	return a
}

func (p *remnawave) Get(ctx context.Context, username string) (*Account, error) {
	var res struct {
		Response map[string]any `json:"response"`
	}
	if err := p.r.do(ctx, http.MethodGet, "/users/by-username/"+url.PathEscape(username), nil, &res); err != nil {
		if isStatus(err, http.StatusNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if res.Response == nil {
		return nil, ErrNotFound
	}
	return p.account(res.Response), nil
}

func remnaExpire(e Expiry, now time.Time) string {
	switch e.Kind {
	case At:
		return e.At.UTC().Format(time.RFC3339)
	case Pending: // not supported by the panel: counts from now
		return now.Add(e.Duration).UTC().Format(time.RFC3339)
	}
	return remnaNever.Format(time.RFC3339)
}

func (p *remnawave) Create(ctx context.Context, s Spec) (*Account, error) {
	if _, err := p.Get(ctx, s.Username); err == nil {
		return nil, ErrExists
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	now := time.Now()
	squads, err := p.squads(ctx, s.Groups)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"username":             s.Username,
		"trafficLimitBytes":    s.TotalBytes,
		"trafficLimitStrategy": "NO_RESET",
		"expireAt":             remnaExpire(ExpiryFor(s.Days, s.AfterFirstUse, now), now),
		"activeInternalSquads": squads,
	}
	if s.Note != "" {
		body["description"] = s.Note
	}
	if id, err := strconv.ParseInt(s.TgID, 10, 64); err == nil && id != 0 {
		body["telegramId"] = id
	}
	if s.LimitIP > 0 {
		body["hwidDeviceLimit"] = s.LimitIP
	}
	if s.Disabled {
		body["status"] = "DISABLED"
	}
	var res struct {
		Response map[string]any `json:"response"`
	}
	if err := p.r.do(ctx, http.MethodPost, "/users", body, &res); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already exist") {
			return nil, ErrExists
		}
		return nil, err
	}
	return p.account(res.Response), nil
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// squads validates squad uuids; empty means every internal squad.
func (p *remnawave) squads(ctx context.Context, ids []string) ([]string, error) {
	out := []string{}
	for _, s := range ids {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		if !uuidRe.MatchString(s) {
			return nil, fmt.Errorf("remnawave: squad %q is not a UUID", s)
		}
		out = append(out, s)
	}
	if len(out) > 0 {
		return out, nil
	}
	groups, err := p.Groups(ctx)
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		out = append(out, g.ID)
	}
	return out, nil
}

func (p *remnawave) Update(ctx context.Context, username string, ch Change) (*Account, error) {
	a, err := p.Get(ctx, username)
	if err != nil {
		return nil, err
	}
	old := *a
	now := time.Now()
	renewed := ch.Apply(a, now)
	body := map[string]any{}
	if uuidRe.MatchString(a.ref) {
		body["uuid"] = a.ref // 1.x/2.x address users by uuid
	} else {
		body["username"] = a.Username
	}
	changed := false
	if renewed {
		body["trafficLimitBytes"] = a.Total
		body["expireAt"] = remnaExpire(a.Expire, now)
		changed = true
		if old.Status == "expired" || old.Status == "limited" {
			body["status"] = "ACTIVE"
		}
	}
	if ch.Enable != nil {
		body["status"], changed = map[bool]string{true: "ACTIVE", false: "DISABLED"}[*ch.Enable], true
	}
	if ch.Note != nil {
		body["description"], changed = *ch.Note, true
	}
	if ch.LimitIP != nil {
		body["hwidDeviceLimit"], changed = *ch.LimitIP, true
	}
	if changed {
		if err := p.r.do(ctx, http.MethodPatch, "/users", body, nil); err != nil {
			return nil, err
		}
	}
	if ch.ResetUsage {
		if err := p.r.do(ctx, http.MethodPost, "/users/"+url.PathEscape(a.ref)+"/actions/reset-traffic", nil, nil); err != nil {
			return nil, err
		}
	}
	return p.Get(ctx, username)
}

func (p *remnawave) Delete(ctx context.Context, username string) error {
	a, err := p.Get(ctx, username)
	if err != nil {
		return err
	}
	return p.r.do(ctx, http.MethodDelete, "/users/"+url.PathEscape(a.ref), nil, nil)
}

func (p *remnawave) list(ctx context.Context) ([]*Account, error) {
	const size = 1000
	var out []*Account
	for start := 0; ; start += size {
		var res struct {
			Response struct {
				Users []map[string]any `json:"users"`
				Total int              `json:"total"`
			} `json:"response"`
		}
		if err := p.r.do(ctx, http.MethodGet, fmt.Sprintf("/users?start=%d&size=%d", start, size), nil, &res); err != nil {
			return nil, err
		}
		for _, u := range res.Response.Users {
			out = append(out, p.account(u))
		}
		if len(res.Response.Users) < size || len(out) >= res.Response.Total {
			return out, nil
		}
	}
}

func (p *remnawave) Find(ctx context.Context, q Query) ([]*Account, error) {
	return filter(p.list(ctx))(q)
}

func (p *remnawave) Onlines(ctx context.Context) ([]string, error) { return onlinesOf(p.list(ctx)) }

func (p *remnawave) Groups(ctx context.Context) ([]Group, error) {
	var res struct {
		Response struct {
			InternalSquads []map[string]any `json:"internalSquads"`
		} `json:"response"`
	}
	if err := p.r.do(ctx, http.MethodGet, "/internal-squads", nil, &res); err != nil {
		return nil, err
	}
	out := make([]Group, len(res.Response.InternalSquads))
	for i, sq := range res.Response.InternalSquads {
		info, _ := sq["info"].(map[string]any)
		out[i] = Group{ID: str(sq["uuid"]), Name: str(sq["name"]), Users: int(num(info["membersCount"])), Enabled: true}
	}
	return out, nil
}

func (p *remnawave) Call(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var raw json.RawMessage
	err := p.r.do(ctx, method, "/"+strings.TrimPrefix(strings.TrimPrefix(path, "/"), "api/"), body, &raw)
	return raw, err
}
