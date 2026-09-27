package vpn_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mrjvadi/tgcreator/internal/vpn"
	"github.com/mrjvadi/tgcreator/internal/vpn/vpnfake"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// Every panel runs the same account lifecycle through the shared interface.
func TestPanelContract(t *testing.T) {
	for _, typ := range workflow.VPNTypes {
		t.Run(typ, func(t *testing.T) {
			ctx := context.Background()
			fake := vpnfake.New(typ)
			defer fake.Close()
			p, err := vpn.New(fake.Config())
			if err != nil {
				t.Fatal(err)
			}
			groups, err := p.Groups(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if typ != "hiddify" && len(groups) == 0 {
				t.Fatal("no groups")
			}
			spec := vpn.Spec{Username: "tg42", TotalBytes: 5 * vpn.GB, Days: 30, TgID: "42", Note: "Sara"}
			if strings.Contains(typ, "x-ui") {
				spec.Groups = []string{"1"}
			}
			a, err := p.Create(ctx, spec)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			info := a.Info(now)
			noteOK := a.Note == "Sara" || typ == "x-ui" // alireza0's x-ui stores no comment
			if a.Username != "tg42" || a.Total != 5*vpn.GB || info["days_left"].(float64) < 29.8 || a.TgID != "42" || !noteOK || !a.Enabled || a.SubLink == "" {
				t.Fatalf("created: %+v %v", a, info)
			}
			if _, err := p.Create(ctx, spec); !errors.Is(err, vpn.ErrExists) {
				t.Fatalf("duplicate: %v", err)
			}

			fake.SetUsage("tg42", vpn.GB*3/2)
			a, err = p.Get(ctx, "tg42")
			if err != nil {
				t.Fatal(err)
			}
			if info = a.Info(now); info["used_gb"] != 1.5 || info["remaining_gb"] != 3.5 || info["active"] != true {
				t.Fatalf("usage: %v", info)
			}

			mine, err := p.Find(ctx, vpn.Query{TgID: "42"})
			if err != nil || len(mine) != 1 || mine[0].Username != "tg42" {
				t.Fatalf("find by tg: %v %v", mine, err)
			}
			if none, _ := p.Find(ctx, vpn.Query{TgID: "99"}); len(none) != 0 {
				t.Fatalf("find other tg: %v", none)
			}

			a, err = p.Update(ctx, "tg42", vpn.Change{AddDays: 10, AddBytes: 5 * vpn.GB, ResetUsage: true})
			if err != nil {
				t.Fatal(err)
			}
			if info = a.Info(now); a.Total != 10*vpn.GB || a.Used != 0 || info["days_left"].(float64) < 39.8 || info["days_left"].(float64) > 41 { // Hiddify counts whole days
				t.Fatalf("renewed: %+v %v", a, info)
			}
			if a, err = p.Update(ctx, "tg42", vpn.Change{Enable: ptr(false)}); err != nil || a.Enabled || a.Info(now)["active"] != false {
				t.Fatalf("disable: %+v %v", a, err)
			}
			if a, err = p.Update(ctx, "tg42", vpn.Change{Enable: ptr(true)}); err != nil || !a.Enabled {
				t.Fatalf("enable: %+v %v", a, err)
			}

			// Starts at the first connection (Remnawave has no such mode).
			pend, err := p.Create(ctx, vpn.Spec{Username: "pend", Days: 7, AfterFirstUse: true, Groups: spec.Groups})
			if err != nil {
				t.Fatal(err)
			}
			wantKind := vpn.Pending
			if typ == "remnawave" {
				wantKind = vpn.At
			}
			if pend.Expire.Kind != wantKind || pend.Info(now)["days_left"].(float64) < 6.9 {
				t.Fatalf("pending: %+v", pend.Expire)
			}
			if pend, err = p.Update(ctx, "pend", vpn.Change{AddDays: 3}); err != nil || pend.Expire.Kind != wantKind || pend.Info(now)["days_left"].(float64) < 9.9 {
				t.Fatalf("extend pending: %+v %v", pend, err)
			}
			if pend.Total != 0 || pend.Info(now)["unlimited"] != true {
				t.Fatalf("unlimited volume: %+v", pend)
			}

			fake.SetOnline("tg42")
			if on, err := p.Onlines(ctx); err != nil || !slices.Contains(on, "tg42") {
				t.Fatalf("onlines: %v %v", on, err)
			}

			logins := fake.Logins()
			fake.ExpireSessions()
			if _, err := p.Get(ctx, "tg42"); err != nil {
				t.Fatalf("after the panel forgot the session: %v", err)
			}
			if logins > 0 && fake.Logins() != logins+1 {
				t.Fatalf("logins %d → %d", logins, fake.Logins())
			}

			if err := p.Delete(ctx, "tg42"); err != nil {
				t.Fatal(err)
			}
			if _, err := p.Get(ctx, "tg42"); !errors.Is(err, vpn.ErrNotFound) {
				t.Fatalf("after delete: %v", err)
			}
			if err := p.Delete(ctx, "tg42"); !errors.Is(err, vpn.ErrNotFound) {
				t.Fatalf("delete twice: %v", err)
			}
			if _, err := p.Update(ctx, "nobody", vpn.Change{AddDays: 1}); !errors.Is(err, vpn.ErrNotFound) {
				t.Fatalf("update missing: %v", err)
			}
		})
	}
}

func TestExpiryRules(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if e := vpn.ExpiryFor(0, false, now); e.Kind != vpn.Never {
		t.Fatal("0 days = never")
	}
	if e := (vpn.Expiry{Kind: vpn.Never}).Extend(5, now); e.Kind != vpn.Never {
		t.Fatal("unlimited stays unlimited")
	}
	if e := (vpn.Expiry{Kind: vpn.Pending, Duration: vpn.Day}).Extend(5, now); e.Duration != 6*vpn.Day {
		t.Fatal("pending grows")
	}
	if e := (vpn.Expiry{Kind: vpn.At, At: now.Add(-vpn.Day)}).Extend(5, now); !e.At.Equal(now.Add(5 * vpn.Day)) {
		t.Fatal("expired restarts from now")
	}
	if e := (vpn.Expiry{Kind: vpn.At, At: now.Add(vpn.Day)}).Extend(5, now); !e.At.Equal(now.Add(6 * vpn.Day)) {
		t.Fatal("active adds to the end")
	}
}

func TestJalali(t *testing.T) {
	cases := map[string]string{"2024-03-20": "1403/01/01", "2026-09-27": "1405/07/05", "2000-01-01": "1378/10/11", "2025-03-21": "1404/01/01"}
	for g, j := range cases {
		d, _ := time.Parse("2006-01-02", g)
		if got := vpn.Jalali(d); got != j {
			t.Errorf("Jalali(%s) = %s, want %s", g, got, j)
		}
	}
}

// Hiddify: the API key is taken from a pasted admin link.
func TestHiddifyAdminLink(t *testing.T) {
	fake := vpnfake.New("hiddify")
	defer fake.Close()
	cfg := fake.Config()
	cfg.Token = ""
	p, err := vpn.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Groups(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Find(context.Background(), vpn.Query{}); err != nil {
		t.Fatalf("empty list: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }
