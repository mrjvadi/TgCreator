package tgsim_test

// End-to-end scenario: examples/vpn-shop sells VPN accounts to six users
// of the Telegram simulator, once per panel type (in-memory fakes of
// 3x-ui, x-ui, Marzban, PasarGuard, Marzneshin, Remnawave and Hiddify).
//
//	TGC_SIM_TRANSCRIPT=/tmp/vpn go test ./internal/tgsim/ -run VPN -v

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mrjvadi/tgcreator/internal/engine"
	"github.com/mrjvadi/tgcreator/internal/tg"
	"github.com/mrjvadi/tgcreator/internal/tgsim"
	"github.com/mrjvadi/tgcreator/internal/vpn"
	"github.com/mrjvadi/tgcreator/internal/vpn/vpnfake"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

func TestScenarioVPNShop(t *testing.T) {
	for _, typ := range workflow.VPNTypes {
		t.Run(typ, func(t *testing.T) { vpnShop(t, typ) })
	}
}

func vpnShop(t *testing.T, typ string) {
	panel := vpnfake.New(typ)
	defer panel.Close()
	xui := strings.Contains(typ, "x-ui")

	sim := tgsim.New("vpn_shop_bot")
	defer sim.Close()
	for _, u := range []struct {
		id         int64
		name, nick string
	}{{ali, "Ali", "ali_dev"}, {sara, "Sara", "sara_go"}, {reza, "Reza", ""}, {mehdi, "Mehdi", "mehdi"}, {neda, "Neda", "neda"}, {omid, "Omid", "omid"}} {
		sim.AddUser(u.id, u.name, u.nick)
	}

	wf, err := workflow.Load("../../examples/vpn-shop/workflow.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg := panel.Config()
	cfg.Address = "vpn.example.com"
	wf.Services.VPN["main"] = cfg
	if !xui {
		wf.Variables["inbound"] = "" // every group / inbound of the panel
	}
	e, err := engine.New(wf, engine.Options{Client: tg.New(sim.Token, sim.URL), OnUpdateHandled: sim.Handled})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := e.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	runErr := make(chan error, 1)
	go func() { runErr <- e.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-runErr; err != nil {
			t.Error(err)
		}
	}()
	defer func() {
		if prefix := os.Getenv("TGC_SIM_TRANSCRIPT"); prefix != "" {
			_ = os.WriteFile(prefix+"-"+typ+".md", []byte("# VPN shop transcript ("+typ+")\n"+sim.Transcript()), 0o644)
		}
	}()
	sc := &scenario{t: t, sim: sim}
	time.Sleep(50 * time.Millisecond)

	// daysLeft checks a user's volume and remaining days on the panel.
	check := func(name string, gb int64, days float64) {
		t.Helper()
		limit, exp, enabled := panel.Limit(name)
		if limit != gb*vpn.GB || !enabled {
			t.Fatalf("%s: limit %d GB, enabled %v; want %d GB", name, limit/vpn.GB, enabled, gb)
		}
		// Hiddify counts whole days, rounded up.
		if left := time.Until(exp).Hours() / 24; left < days-0.1 || left > days+1 {
			t.Fatalf("%s: %.2f days left, want %.0f", name, left, days)
		}
	}

	// 1. Sara takes a free trial.
	var menu *tgsim.Msg
	sc.step("۱. سارا: اکانت تست رایگان", func() {
		sim.Send(sara, sara, "/start")
		sc.wait()
		menu = sc.lastBot(sara, "فروشگاه VPN")
		sc.press(sara, menu, "🎁 اکانت تست رایگان")
	})
	got := sc.lastBot(sara, "اکانت تست شما آماده است")
	want := []string{"حجم: 1 گیگ", "لینک اشتراک", "\n" + "http"}
	if xui || typ == "marzban" { // panels that return direct config links
		want = append(want, "کانفیگ مستقیم", "vless://", "@vpn.example.com:443")
	} else if strings.Contains(got.Text, "کانفیگ مستقیم") {
		t.Fatalf("no direct link, so no heading for it:\n%s", got.Text)
	}
	for _, w := range want {
		if !strings.Contains(got.Text, w) {
			t.Fatalf("trial message lacks %q:\n%s", w, got.Text)
		}
	}
	check("trial_102", 1, 1)

	// 2. A second trial is refused; her accounts are listed.
	sc.step("۲. سارا: تست دوباره و «اکانت‌های من»", func() {
		sc.press(sara, menu, "🎁 اکانت تست رایگان")
		sc.wait()
		dup := sc.lastBot(sara, "قبلاً اکانت تست گرفته‌اید")
		sc.press(sara, dup, "📊 اکانت‌های من")
	})
	list := sc.lastBot(sara, "اکانت‌های شما")
	for _, w := range []string{"trial_102", "🟢 فعال", "مصرف: 0 از 1 گیگ", "اعتبار تا 14"} {
		if !strings.Contains(list.Text, w) {
			t.Fatalf("account list lacks %q:\n%s", w, list.Text)
		}
	}

	// 3. Reza has nothing yet, then takes a trial from that message.
	sc.step("۳. رضا: بدون اکانت ← دکمهٔ تست", func() {
		sim.Send(reza, reza, "/start")
		sc.wait()
		sc.press(reza, sc.lastBot(reza, "فروشگاه"), "📊 اکانت‌های من")
		sc.wait()
		sc.press(reza, sc.lastBot(reza, "هنوز اکانتی ندارید"), "🎁 اکانت تست رایگان")
	})
	sc.lastBot(reza, "اکانت تست شما آماده است")

	// 4. Mehdi buys with Telegram Stars: a wrong amount is refused.
	sc.step("۴. مهدی: خرید با ستاره", func() {
		sim.Send(mehdi, mehdi, "/start")
		sc.wait()
		sc.press(mehdi, sc.lastBot(mehdi, "فروشگاه"), "🛒 خرید اشتراک ۳۰ روزه")
		sc.wait()
		bad := sim.PreCheckout(mehdi, "vpn30:104", "XTR", 5)
		sc.wait()
		good := sim.PreCheckout(mehdi, "vpn30:104", "XTR", 100)
		sc.wait()
		if !sim.Answered(good) || !sim.Answered(bad) {
			t.Fatal("pre-checkout queries must be answered")
		}
		sim.Paid(mehdi, "vpn30:104", "XTR", 100)
	})
	sc.lastBot(mehdi, "اشتراک شما فعال شد")
	check("vpn_104", 50, 30)

	// 5. He uses 12 GB and pays again: renewed, usage reset, 60 days left.
	panel.SetUsage("vpn_104", 12*vpn.GB)
	sc.step("۵. مهدی: تمدید با پرداخت دوباره", func() {
		sim.Paid(mehdi, "vpn30:104", "XTR", 100)
	})
	sc.lastBot(mehdi, "اشتراک شما تمدید شد")
	check("vpn_104", 50, 60)

	// 6. Neda burns through her trial: listed as inactive.
	sc.step("۶. ندا: حجم تست تمام می‌شود", func() {
		sim.Send(neda, neda, "/start")
		sc.wait()
		m := sc.lastBot(neda, "فروشگاه")
		sc.press(neda, m, "🎁 اکانت تست رایگان")
		sc.wait()
		panel.SetUsage("trial_105", vpn.GB*3/2)
		sc.press(neda, m, "📊 اکانت‌های من")
	})
	if l := sc.lastBot(neda, "اکانت‌های شما"); !strings.Contains(l.Text, "🔴 غیرفعال") || !strings.Contains(l.Text, "مصرف: 1.5 از 1 گیگ") {
		t.Fatalf("depleted trial:\n%s", l.Text)
	}

	// 7. The owner's commands; Omid may not use them.
	panel.SetOnline("trial_102", "vpn_104")
	sc.step("۷. علی (مدیر): /online، /renew، /del", func() {
		sim.Send(ali, ali, "/online")
		sc.wait()
		sc.lastBot(ali, "2 کاربر آنلاین")
		sim.Send(ali, ali, "/renew trial_105 7 5")
		sc.wait()
		sc.lastBot(ali, "trial_105 تمدید شد؛ اعتبار تا 14")
		sim.Send(ali, ali, "/renew nobody 3")
		sc.wait()
		sc.lastBot(ali, "در پنل نیست")
		sim.Send(ali, ali, "/del trial_103")
		sc.wait()
		sc.lastBot(ali, "حذف شد")
		sim.Send(omid, omid, "/online")
	})
	if sim.LastBot(omid) != nil {
		t.Fatal("a non-owner must not get admin replies")
	}
	check("trial_105", 6, 8)
	if panel.Exists("trial_103") {
		t.Fatal("trial_103 should be deleted")
	}

	// 8. The panel restarts (sessions lost): the bot logs in again.
	logins := panel.Logins()
	panel.ExpireSessions()
	sc.step("۸. ری‌استارت پنل: ورود دوباره خودکار", func() {
		sc.press(sara, menu, "📊 اکانت‌های من")
	})
	sc.lastBot(sara, "trial_102")
	if logins > 0 && panel.Logins() != logins+1 {
		t.Fatalf("logins %d → %d, want one more", logins, panel.Logins())
	}

	// 9. The panel is down: users get a friendly error.
	panel.Close()
	sc.step("۹. پنل از دسترس خارج است", func() {
		sim.Send(omid, omid, "/start")
		sc.wait()
		sc.press(omid, sc.lastBot(omid, "فروشگاه"), "🎁 اکانت تست رایگان")
	})
	sc.lastBot(omid, "ارتباط با سرور برقرار نشد")

	if errs := sim.APIErrors(); len(errs) > 0 {
		t.Fatalf("Bot API errors: %+v", errs)
	}
}
