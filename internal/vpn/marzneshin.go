package vpn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// marzneshin adapts Marzneshin: OAuth2 token at /api/admins/token, users
// at /api/users/{username} (lowercase), services instead of inbounds and
// an expire_strategy of never / fixed_date / start_on_first_use.
type marzneshin struct {
	r   *rest
	cfg workflow.VPNPanel
}

func newMarzneshin(cfg workflow.VPNPanel) (Panel, error) {
	cfg = panelRoot(cfg, "/dashboard")
	r, err := newRest(cfg, "/api")
	if err != nil {
		return nil, err
	}
	if cfg.Username == "" {
		return nil, errors.New("panel username is empty")
	}
	r.login = r.formLogin("/admins/token", cfg.Username, cfg.Password)
	return &marzneshin{r: r, cfg: cfg}, nil
}

func (m *marzneshin) Type() string { return "marzneshin" }

func (m *marzneshin) userPath(u string) string {
	return "/users/" + url.PathEscape(strings.ToLower(u))
}

func (m *marzneshin) account(u map[string]any) *Account {
	enabled, _ := u["enabled"].(bool)
	expired, _ := u["expired"].(bool)
	limited, _ := u["data_limit_reached"].(bool)
	a := &Account{
		Username: str(u["username"]),
		ID:       str(u["key"]),
		Enabled:  enabled,
		Used:     int64(num(u["used_traffic"])),
		Total:    int64(num(u["data_limit"])),
		OnlineAt: parseTime(u["online_at"]),
		SubLink:  absURL(str(u["subscription_url"]), m.r.origin, m.cfg.SubURL),
	}
	note := str(u["note"])
	a.TgID = tgFromNote(note)
	a.Note = strings.TrimSpace(tgMark.ReplaceAllString(note, ""))
	for _, s := range asList(u["service_ids"]) {
		a.Groups = append(a.Groups, str(s))
	}
	switch str(u["expire_strategy"]) {
	case "fixed_date":
		a.Expire = Expiry{Kind: At, At: parseTime(u["expire_date"])}
	case "start_on_first_use":
		a.Expire = Expiry{Kind: Pending, Duration: time.Duration(num(u["usage_duration"])) * time.Second}
	}
	switch {
	case !enabled:
		a.Status = "disabled"
	case expired:
		a.Status = "expired"
	case limited:
		a.Status = "limited"
	case a.Expire.Kind == Pending:
		a.Status = "on_hold"
	default:
		a.Status = "active"
	}
	return a
}

func (m *marzneshin) Get(ctx context.Context, username string) (*Account, error) {
	var u map[string]any
	if err := m.r.do(ctx, http.MethodGet, m.userPath(username), nil, &u); err != nil {
		if isStatus(err, http.StatusNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return m.account(u), nil
}

// expiryFields sets the expire strategy. usage_duration is always sent:
// its model default is the tuple (None,), which the panel's update_user
// would otherwise store.
func expiryFields(e Expiry, body map[string]any) {
	body["usage_duration"] = nil
	switch e.Kind {
	case At:
		body["expire_strategy"], body["expire_date"] = "fixed_date", e.At.UTC().Format(time.RFC3339)
	case Pending:
		body["expire_strategy"], body["usage_duration"] = "start_on_first_use", int64(e.Duration.Seconds())
	default:
		body["expire_strategy"] = "never"
	}
}

func (m *marzneshin) Create(ctx context.Context, s Spec) (*Account, error) {
	services, err := m.serviceIDs(ctx, s.Groups)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"username":                  strings.ToLower(s.Username),
		"data_limit":                s.TotalBytes,
		"data_limit_reset_strategy": "no_reset",
		"note":                      noteWithTg(s.Note, s.TgID),
		"service_ids":               services,
	}
	expiryFields(ExpiryFor(s.Days, s.AfterFirstUse, time.Now()), body)
	var u map[string]any
	if err := m.r.do(ctx, http.MethodPost, "/users", body, &u); err != nil {
		if isStatus(err, http.StatusConflict) {
			return nil, ErrExists
		}
		return nil, err
	}
	if s.Disabled {
		return m.Update(ctx, s.Username, Change{Enable: ptr(false)})
	}
	return m.account(u), nil
}

func (m *marzneshin) serviceIDs(ctx context.Context, ids []string) ([]int, error) {
	out := []int{}
	for _, s := range ids {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return nil, fmt.Errorf("marzneshin: service id %q is not a number", s)
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
		n, _ := strconv.Atoi(g.ID)
		out = append(out, n)
	}
	return out, nil
}

func (m *marzneshin) Update(ctx context.Context, username string, ch Change) (*Account, error) {
	a, err := m.Get(ctx, username)
	if err != nil {
		return nil, err
	}
	renewed := ch.Apply(a, time.Now())
	body := map[string]any{"username": a.Username, "usage_duration": nil}
	if renewed {
		body["data_limit"] = a.Total
		expiryFields(a.Expire, body)
	}
	if ch.Note != nil {
		body["note"] = noteWithTg(a.Note, a.TgID)
	}
	if len(body) > 2 {
		if err := m.r.do(ctx, http.MethodPut, m.userPath(username), body, nil); err != nil {
			return nil, err
		}
	}
	if ch.ResetUsage {
		if err := m.r.do(ctx, http.MethodPost, m.userPath(username)+"/reset", nil, nil); err != nil {
			return nil, err
		}
	}
	if ch.Enable != nil {
		action := "/disable"
		if *ch.Enable {
			action = "/enable"
		}
		if err := m.r.do(ctx, http.MethodPost, m.userPath(username)+action, nil, nil); err != nil {
			return nil, err
		}
	}
	return m.Get(ctx, username)
}

func (m *marzneshin) Delete(ctx context.Context, username string) error {
	err := m.r.do(ctx, http.MethodDelete, m.userPath(username), nil, nil)
	if isStatus(err, http.StatusNotFound) {
		return ErrNotFound
	}
	return err
}

type page struct {
	Items []map[string]any `json:"items"`
	Total int              `json:"total"`
	Pages int              `json:"pages"`
}

func (m *marzneshin) Find(ctx context.Context, q Query) ([]*Account, error) {
	return filter(m.list(ctx, q.Search))(q)
}

func (m *marzneshin) list(ctx context.Context, search string) ([]*Account, error) {
	var out []*Account
	for p := 1; ; p++ {
		q := url.Values{"page": {strconv.Itoa(p)}, "size": {"100"}}
		if search != "" {
			q.Set("username", strings.ToLower(search)) // the only server-side text filter
		}
		var res page
		if err := m.r.do(ctx, http.MethodGet, "/users?"+q.Encode(), nil, &res); err != nil {
			return nil, err
		}
		for _, u := range res.Items {
			out = append(out, m.account(u))
		}
		if len(res.Items) == 0 || p >= res.Pages {
			return out, nil
		}
	}
}

func (m *marzneshin) Onlines(ctx context.Context) ([]string, error) {
	return onlinesOf(m.list(ctx, ""))
}

func (m *marzneshin) Groups(ctx context.Context) ([]Group, error) {
	var res page
	if err := m.r.do(ctx, http.MethodGet, "/services?size=100", nil, &res); err != nil {
		return nil, err
	}
	out := make([]Group, len(res.Items))
	for i, s := range res.Items {
		out[i] = Group{ID: str(s["id"]), Name: str(s["name"]), Users: len(asList(s["user_ids"])), Enabled: true}
	}
	return out, nil
}

func (m *marzneshin) Call(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var raw json.RawMessage
	err := m.r.do(ctx, method, "/"+strings.TrimPrefix(strings.TrimPrefix(path, "/"), "api/"), body, &raw)
	return raw, err
}
