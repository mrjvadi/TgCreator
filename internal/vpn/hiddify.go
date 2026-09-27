package vpn

import (
	"context"
	"crypto/sha1" //nolint:gosec // name-based UUID, not security
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// hiddify adapts Hiddify Manager's API v2 (hiddifypanel 10+): header
// Hiddify-API-Key (an admin UUID), routes under
// /<admin proxy path>/api/v2/admin/, users addressed by UUID. A user's
// period is start_date + package_days; no start_date means it starts at
// the first connection. Names are not unique in Hiddify, so users this
// bot creates get a UUID derived from the name and are found directly.
type hiddify struct {
	r   *rest
	cfg workflow.VPNPanel
}

const (
	hiddifyUnlimitedGB   = 100000.0
	hiddifyUnlimitedDays = 10000
)

func newHiddify(cfg workflow.VPNPanel) (Panel, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.URL))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("panel url %q is not an http(s) address", cfg.URL)
	}
	// The admin link is https://domain/<proxy path>/<admin uuid>/admin/...
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if parts[0] == "" {
		return nil, errors.New("hiddify: the url must include the admin proxy path (https://domain/PATH/)")
	}
	if cfg.Token == "" && len(parts) > 1 && uuidRe.MatchString(parts[1]) {
		cfg.Token = parts[1]
	}
	if cfg.Token == "" {
		return nil, errors.New("hiddify: set the API key (an admin UUID) or paste the full admin link")
	}
	cfg.URL = u.Scheme + "://" + u.Host + "/" + parts[0]
	r, err := newRest(cfg, "/api/v2/admin")
	if err != nil {
		return nil, err
	}
	r.headers["Hiddify-API-Key"] = cfg.Token
	return &hiddify{r: r, cfg: cfg}, nil
}

func (h *hiddify) Type() string { return "hiddify" }

// nameUUID is the UUID this bot gives the user called name.
func nameUUID(name string) string {
	sum := sha1.Sum([]byte("tgcreator:" + name)) //nolint:gosec
	sum[6] = sum[6]&0x0f | 0x50
	sum[8] = sum[8]&0x3f | 0x80
	x := hex.EncodeToString(sum[:16])
	return x[0:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:32]
}

func (h *hiddify) account(u map[string]any) *Account {
	enabled := u["enable"] != false
	a := &Account{
		Username: str(u["name"]),
		ID:       str(u["uuid"]),
		Enabled:  enabled,
		Used:     int64(num(u["current_usage_GB"]) * float64(GB)),
		OnlineAt: parseTime(u["last_online"]),
		Note:     str(u["comment"]),
		TgID:     str(u["telegram_id"]),
		ref:      str(u["uuid"]),
	}
	if a.TgID == "0" {
		a.TgID = ""
	}
	if gb := num(u["usage_limit_GB"]); gb < hiddifyUnlimitedGB {
		a.Total = int64(gb * float64(GB))
	}
	days := num(u["package_days"])
	start := parseTime(u["start_date"])
	switch {
	case days >= hiddifyUnlimitedDays:
	case start.IsZero():
		a.Expire = Expiry{Kind: Pending, Duration: DaysDuration(days)}
	default:
		a.Expire = Expiry{Kind: At, At: start.AddDate(0, 0, int(days))}
	}
	if h.cfg.SubURL != "" {
		a.SubLink = strings.TrimRight(h.cfg.SubURL, "/") + "/" + a.ID + "/"
	}
	now := time.Now()
	switch {
	case !enabled:
		a.Status = "disabled"
	case a.Expire.Kind == At && a.Expire.At.Before(now):
		a.Status = "expired"
	case a.Total > 0 && a.Used >= a.Total:
		a.Status = "limited"
	case a.Expire.Kind == Pending:
		a.Status = "on_hold"
	default:
		a.Status = "active"
	}
	return a
}

func (h *hiddify) byUUID(ctx context.Context, id string) (*Account, error) {
	var u map[string]any
	if err := h.r.do(ctx, http.MethodGet, "/user/"+id+"/", nil, &u); err != nil {
		if isStatus(err, http.StatusNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return h.account(u), nil
}

func (h *hiddify) Get(ctx context.Context, username string) (*Account, error) {
	if uuidRe.MatchString(username) {
		return h.byUUID(ctx, strings.ToLower(username))
	}
	a, err := h.byUUID(ctx, nameUUID(username))
	if !errors.Is(err, ErrNotFound) {
		return a, err
	}
	// Created elsewhere: look the name up.
	list, err := h.list(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range list {
		if a.Username == username {
			return a, nil
		}
	}
	return nil, ErrNotFound
}

// period converts an expiry to start_date and package_days.
func hiddifyPeriod(e Expiry, now time.Time) (start any, days int) {
	midnight := now.UTC().Truncate(24 * time.Hour)
	today := midnight.Format("2006-01-02")
	switch e.Kind {
	case At:
		// Whole days from today, rounded up: never shorter than asked.
		d := int(math.Ceil(e.At.Sub(midnight).Hours() / 24))
		return today, max(d, 0)
	case Pending:
		return nil, max(int(math.Ceil(e.Duration.Hours()/24)), 1)
	}
	return today, hiddifyUnlimitedDays
}

func hiddifyGB(total int64) float64 {
	if total <= 0 {
		return hiddifyUnlimitedGB
	}
	return math.Round(float64(total)/float64(GB)*1000) / 1000
}

func (h *hiddify) Create(ctx context.Context, s Spec) (*Account, error) {
	if _, err := h.Get(ctx, s.Username); err == nil {
		return nil, ErrExists
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	now := time.Now()
	start, days := hiddifyPeriod(ExpiryFor(s.Days, s.AfterFirstUse, now), now)
	body := map[string]any{
		"uuid":           nameUUID(s.Username),
		"name":           s.Username,
		"usage_limit_GB": hiddifyGB(s.TotalBytes),
		"package_days":   days,
		"mode":           "no_reset",
		"enable":         !s.Disabled,
	}
	if start != nil {
		body["start_date"] = start
	}
	if s.Note != "" {
		body["comment"] = s.Note
	}
	if s.TgID != "" {
		var id int64
		if _, err := fmt.Sscan(s.TgID, &id); err == nil && id != 0 {
			body["telegram_id"] = id
		}
	}
	var u map[string]any
	if err := h.r.do(ctx, http.MethodPost, "/user/", body, &u); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "exists") {
			return nil, ErrExists
		}
		return nil, err
	}
	return h.account(u), nil
}

func (h *hiddify) Update(ctx context.Context, username string, ch Change) (*Account, error) {
	a, err := h.Get(ctx, username)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	body := map[string]any{}
	if ch.Apply(a, now) {
		start, days := hiddifyPeriod(a.Expire, now)
		body["usage_limit_GB"], body["package_days"], body["start_date"] = hiddifyGB(a.Total), days, start
	}
	if ch.ResetUsage {
		body["current_usage_GB"] = 0
	}
	if ch.Enable != nil {
		body["enable"] = *ch.Enable
	}
	if ch.Note != nil {
		body["comment"] = *ch.Note
	}
	if len(body) > 0 {
		var u map[string]any
		if err := h.r.do(ctx, http.MethodPatch, "/user/"+a.ref+"/", body, &u); err != nil {
			return nil, err
		}
	}
	return h.byUUID(ctx, a.ref)
}

func (h *hiddify) Delete(ctx context.Context, username string) error {
	a, err := h.Get(ctx, username)
	if err != nil {
		return err
	}
	return h.r.do(ctx, http.MethodDelete, "/user/"+a.ref+"/", nil, nil)
}

func (h *hiddify) list(ctx context.Context) ([]*Account, error) {
	var users []map[string]any
	if err := h.r.do(ctx, http.MethodGet, "/user/", nil, &users); err != nil {
		if isStatus(err, http.StatusNotFound) { // "You have no user"
			return nil, nil
		}
		return nil, err
	}
	out := make([]*Account, len(users))
	for i, u := range users {
		out[i] = h.account(u)
	}
	return out, nil
}

func (h *hiddify) Find(ctx context.Context, q Query) ([]*Account, error) {
	return filter(h.list(ctx))(q)
}

func (h *hiddify) Onlines(ctx context.Context) ([]string, error) { return onlinesOf(h.list(ctx)) }

// Groups: Hiddify gives every user all of the server's protocols.
func (h *hiddify) Groups(context.Context) ([]Group, error) { return []Group{}, nil }

func (h *hiddify) Call(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var raw json.RawMessage
	p := "/" + strings.TrimPrefix(strings.TrimPrefix(path, "/"), "api/v2/admin/")
	err := h.r.do(ctx, method, p, body, &raw)
	return raw, err
}
