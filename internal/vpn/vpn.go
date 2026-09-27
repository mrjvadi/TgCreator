// Package vpn manages users on VPN panels behind one interface: X-UI
// (3x-ui, alireza0 x-ui), Marzban, PasarGuard, Marzneshin, Remnawave and
// Hiddify. Each adapter follows its panel's own API (checked against the
// panels' source code); this file holds the shared model and the rules
// for renewing an account, so every panel behaves the same in a bot.
package vpn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// ErrNotFound means the panel has no user with that name.
var ErrNotFound = errors.New("user not found")

// ErrExists means a user with that name already exists.
var ErrExists = errors.New("user already exists")

// Panel is one VPN panel. Implementations are safe for concurrent use.
type Panel interface {
	Type() string
	Create(ctx context.Context, s Spec) (*Account, error)
	Get(ctx context.Context, username string) (*Account, error)
	Update(ctx context.Context, username string, c Change) (*Account, error)
	Delete(ctx context.Context, username string) error
	// Find lists users. Panels narrow the list server side where they
	// can; callers apply the query exactly with Query.Match.
	Find(ctx context.Context, q Query) ([]*Account, error)
	Onlines(ctx context.Context) ([]string, error)
	// Groups are what a new user can be attached to: X-UI inbounds,
	// Marzban inbound tags, PasarGuard groups, Marzneshin services,
	// Remnawave internal squads.
	Groups(ctx context.Context) ([]Group, error)
	// Call sends any request to the panel's API with the panel's auth.
	Call(ctx context.Context, method, path string, body any) (json.RawMessage, error)
}

// Group is an inbound, group, service or squad a user may join.
type Group struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol,omitempty"`
	Port     int    `json:"port,omitempty"`
	Users    int    `json:"users"`
	Enabled  bool   `json:"enabled"`
}

// Spec describes a user to create.
type Spec struct {
	Username      string
	TotalBytes    int64   // 0 = unlimited
	Days          float64 // 0 = never expires
	AfterFirstUse bool    // the days start at the first connection
	LimitIP       int
	Flow          string
	TgID          string
	Note          string
	Groups        []string // empty = the panel's default (all, except X-UI)
	Disabled      bool
	SubID         string // X-UI only
}

// Change is an update; nil fields are left alone.
type Change struct {
	AddDays    float64
	SetDays    *float64 // from now; 0 = never expires
	AddBytes   int64
	SetBytes   *int64 // 0 = unlimited
	ResetUsage bool
	Enable     *bool
	LimitIP    *int
	Note       *string
}

// Query selects users by Telegram id and/or text in the name or note.
type Query struct {
	TgID   string
	Search string
}

// Match reports whether a matches the query.
func (q Query) Match(a *Account) bool {
	if q.TgID != "" && a.TgID != q.TgID {
		return false
	}
	s := strings.ToLower(q.Search)
	return s == "" || strings.Contains(strings.ToLower(a.Username), s) || strings.Contains(strings.ToLower(a.Note), s)
}

// ExpiryKind says how an account expires.
type ExpiryKind int

const (
	Never   ExpiryKind = iota
	At                 // on a fixed date
	Pending            // Duration after the first connection
)

// Expiry is when an account ends.
type Expiry struct {
	Kind     ExpiryKind
	At       time.Time
	Duration time.Duration
}

// Account is a panel user in the shared model.
type Account struct {
	Username string
	ID       string // uuid / password / key, as the panel shows it
	Status   string // active, disabled, expired, limited, on_hold
	Enabled  bool   // not disabled by an admin
	Used     int64
	Total    int64 // 0 = unlimited
	Expire   Expiry
	OnlineAt time.Time
	SubLink  string
	Links    []string
	Note     string
	TgID     string
	LimitIP  int
	Groups   []string
	Protocol string

	ref string // the id the panel's routes use (Remnawave id, Hiddify uuid)
}

const (
	GB  = int64(1) << 30
	Day = 24 * time.Hour
)

// DaysDuration converts days (fractions allowed) to a duration.
func DaysDuration(days float64) time.Duration {
	return time.Duration(math.Round(days * float64(Day)))
}

// ExpiryFor is the expiry of a new account.
func ExpiryFor(days float64, afterFirstUse bool, now time.Time) Expiry {
	switch {
	case days <= 0:
		return Expiry{Kind: Never}
	case afterFirstUse:
		return Expiry{Kind: Pending, Duration: DaysDuration(days)}
	}
	return Expiry{Kind: At, At: now.Add(DaysDuration(days))}
}

// Extend adds days: unlimited stays unlimited, a pending period gets
// longer, an expired account restarts from now.
func (e Expiry) Extend(days float64, now time.Time) Expiry {
	d := DaysDuration(days)
	switch {
	case e.Kind == Never || d == 0:
		return e
	case e.Kind == Pending:
		e.Duration += d
	case e.At.Before(now):
		e.At = now.Add(d)
	default:
		e.At = e.At.Add(d)
	}
	return e
}

// Apply computes the account after a change. renewed reports a change of
// days or volume, which re-activates accounts the panel had stopped.
func (c Change) Apply(a *Account, now time.Time) (renewed bool) {
	if c.SetDays != nil {
		a.Expire, renewed = ExpiryFor(*c.SetDays, false, now), true
	}
	if c.AddDays != 0 {
		a.Expire, renewed = a.Expire.Extend(c.AddDays, now), true
	}
	if c.SetBytes != nil {
		a.Total, renewed = max(*c.SetBytes, 0), true
	}
	if c.AddBytes != 0 && a.Total != 0 {
		a.Total, renewed = max(a.Total+c.AddBytes, 0), true
	}
	if c.ResetUsage {
		a.Used, renewed = 0, true
	}
	if c.Enable != nil {
		a.Enabled = *c.Enable
	}
	if c.LimitIP != nil {
		a.LimitIP = *c.LimitIP
	}
	if c.Note != nil {
		a.Note = *c.Note
	}
	return renewed
}

// Info flattens an account into the values bot messages use.
func (a *Account) Info(now time.Time) map[string]any {
	link := ""
	if len(a.Links) > 0 {
		link = a.Links[0]
	}
	links := make([]any, len(a.Links))
	for i, l := range a.Links {
		links[i] = l
	}
	groups := make([]any, len(a.Groups))
	for i, g := range a.Groups {
		groups[i] = g
	}
	out := map[string]any{
		"username":    a.Username,
		"id":          a.ID,
		"status":      a.Status,
		"enable":      a.Enabled,
		"sub_link":    a.SubLink,
		"link":        link,
		"links":       links,
		"note":        a.Note,
		"comment":     a.Note,
		"tg_id":       a.TgID,
		"limit_ip":    float64(a.LimitIP),
		"groups":      groups,
		"protocol":    a.Protocol,
		"used_bytes":  float64(a.Used),
		"used_gb":     round2(float64(a.Used) / float64(GB)),
		"total_bytes": float64(a.Total),
		"total_gb":    round2(float64(a.Total) / float64(GB)),
		"unlimited":   a.Total == 0,
		"last_online": "",
		"online":      !a.OnlineAt.IsZero() && now.Sub(a.OnlineAt) < OnlineWindow,
	}
	if !a.OnlineAt.IsZero() {
		out["last_online"] = a.OnlineAt.In(Tehran).Format("2006-01-02 15:04")
	}
	depleted := false
	if a.Total > 0 {
		rem := max(a.Total-a.Used, 0)
		out["remaining_bytes"], out["remaining_gb"] = float64(rem), round2(float64(rem)/float64(GB))
		depleted = a.Used >= a.Total
	} else {
		out["remaining_bytes"], out["remaining_gb"] = float64(-1), float64(-1)
	}
	out["depleted"] = depleted
	expired := false
	switch a.Expire.Kind {
	case At:
		left := a.Expire.At.Sub(now).Hours() / 24
		expired = left <= 0
		out["days_left"] = math.Max(0, math.Floor(left*10)/10)
		out["started"] = true
		out["never_expire"] = false
		out["expiry_time"] = float64(a.Expire.At.UnixMilli())
		out["expiry_date"] = a.Expire.At.In(Tehran).Format("2006-01-02 15:04")
		out["expiry_jalali"] = Jalali(a.Expire.At.In(Tehran))
	case Pending:
		out["days_left"] = math.Floor(a.Expire.Duration.Hours()/24*10) / 10
		out["started"] = false
		out["never_expire"] = false
		out["expiry_time"] = float64(0)
		out["expiry_date"], out["expiry_jalali"] = "", ""
	default:
		out["days_left"] = float64(-1)
		out["started"] = true
		out["never_expire"] = true
		out["expiry_time"] = float64(0)
		out["expiry_date"], out["expiry_jalali"] = "", ""
	}
	out["expired"] = expired
	out["active"] = a.Enabled && !expired && !depleted && a.Status != "disabled" && a.Status != "expired" && a.Status != "limited"
	return out
}

// OnlineWindow is how recent a connection counts as online.
const OnlineWindow = 3 * time.Minute

// Tehran is Iran Standard Time (no daylight saving since 2022).
var Tehran = time.FixedZone("IRST", 3*3600+1800)

// Jalali formats t as a Solar Hijri date, YYYY/MM/DD.
func Jalali(t time.Time) string {
	gy, gm, gd := t.Date()
	jy, jm, jd := toJalali(gy, int(gm), gd)
	return fmt.Sprintf("%04d/%02d/%02d", jy, jm, jd)
}

// toJalali is the standard arithmetic Gregorian → Solar Hijri conversion.
func toJalali(gy, gm, gd int) (int, int, int) {
	gdm := [...]int{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}
	gy2 := gy
	if gm > 2 {
		gy2 = gy + 1
	}
	days := 355666 + 365*gy + (gy2+3)/4 - (gy2+99)/100 + (gy2+399)/400 + gd + gdm[gm-1]
	jy := -1595 + 33*(days/12053)
	days %= 12053
	jy += 4 * (days / 1461)
	days %= 1461
	if days > 365 {
		jy += (days - 1) / 365
		days = (days - 1) % 365
	}
	var jm, jd int
	if days < 186 {
		jm, jd = 1+days/31, 1+days%31
	} else {
		jm, jd = 7+(days-186)/30, 1+(days-186)%30
	}
	return jy, jm, jd
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

// Panels without a Telegram field keep the id in the note as "tg:<id>".
var tgMark = regexp.MustCompile(`(?:^|\s)tg:(\d+)`)

func noteWithTg(note, tg string) string {
	if tg == "" || tgMark.MatchString(note) {
		return note
	}
	return strings.TrimSpace(note + " tg:" + tg)
}

func tgFromNote(note string) string {
	if m := tgMark.FindStringSubmatch(note); m != nil {
		return m[1]
	}
	return ""
}

// Types lists the supported panels with their Persian names.
var Types = []struct{ ID, Name string }{
	{"3x-ui", "3x-ui (سنایی)"},
	{"x-ui", "x-ui (علیرضا)"},
	{"marzban", "مرزبان"},
	{"pasarguard", "پاسارگاد"},
	{"marzneshin", "مرزنشین"},
	{"remnawave", "رمناویو"},
	{"hiddify", "هیدیفای"},
}

// New connects to a panel of cfg.Type. It does not contact the panel.
func New(cfg workflow.VPNPanel) (Panel, error) {
	switch cfg.Type {
	case "", "3x-ui", "x-ui":
		return newXUI(cfg)
	case "marzban", "pasarguard":
		return newMarzban(cfg)
	case "marzneshin":
		return newMarzneshin(cfg)
	case "remnawave":
		return newRemnawave(cfg)
	case "hiddify":
		return newHiddify(cfg)
	}
	return nil, fmt.Errorf("unsupported panel type %q", cfg.Type)
}
