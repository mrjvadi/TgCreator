package tgsim_test

// End-to-end scenario: examples/vpn-shop sells VPN accounts from a 3x-ui
// panel (in-memory fake) to six users of the Telegram simulator.
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
	"github.com/mrjvadi/tgcreator/internal/workflow"
	"github.com/mrjvadi/tgcreator/internal/xui"
	"github.com/mrjvadi/tgcreator/internal/xui/xuifake"
)

func TestScenarioVPNShop(t *testing.T) {
	panel := xuifake.New("3x-ui")
	defer panel.Close()
	t.Setenv("XUI_URL", panel.URL)
	t.Setenv("XUI_USERNAME", panel.Username)
	t.Setenv("XUI_PASSWORD", panel.Password)
	t.Setenv("XUI_ADDRESS", "vpn.example.com")

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
			_ = os.WriteFile(prefix+".md", []byte("# VPN shop transcript\n"+sim.Transcript()), 0o644)
			_ = os.WriteFile(prefix+".html", []byte(sim.HTMLTranscript("سناریوی فروشگاه VPN")), 0o644)
		}
	}()
	sc := &scenario{t: t, sim: sim}
	time.Sleep(50 * time.Millisecond)

	stored := func(email string) map[string]any {
		t.Helper()
		c, _ := panel.Client(email)
		if c == nil {
			t.Fatalf("panel has no client %q", email)
		}
		return c
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
	c := stored("trial_102")
	for _, want := range []string{"حجم: 1 گیگ", "https://sub.example.com:2096/sub/" + c["subId"].(string), "vless://" + c["id"].(string) + "@vpn.example.com:443"} {
		if !strings.Contains(got.Text, want) {
			t.Fatalf("trial message lacks %q:\n%s", want, got.Text)
		}
	}
	if c["totalGB"] != float64(xui.GB) || c["tgId"] != float64(sara) || c["comment"] != "trial Sara" {
		t.Fatalf("stored trial client: %v", c)
	}
	if left := (c["expiryTime"].(float64) - float64(time.Now().UnixMilli())) / float64(xui.DayMs); left < 0.99 || left > 1.01 {
		t.Fatalf("trial should last 1 day, lasts %.2f", left)
	}

	// 2. A second trial is refused; her accounts are listed.
	sc.step("۲. سارا: تست دوباره و «اکانت‌های من»", func() {
		sc.press(sara, menu, "🎁 اکانت تست رایگان")
		sc.wait()
		dup := sc.lastBot(sara, "قبلاً اکانت تست گرفته‌اید")
		sc.press(sara, dup, "📊 اکانت‌های من")
	})
	list := sc.lastBot(sara, "اکانت‌های شما")
	for _, want := range []string{"trial_102", "🟢 فعال", "مصرف: 0 از 1 گیگ", "اعتبار تا 14"} {
		if !strings.Contains(list.Text, want) {
			t.Fatalf("account list lacks %q:\n%s", want, list.Text)
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
		if inv := sim.CallsTo("sendInvoice"); len(inv) != 1 {
			t.Fatalf("sendInvoice calls: %d", len(inv))
		}
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
	c = stored("vpn_104")
	if c["totalGB"] != float64(50*xui.GB) || c["limitIp"] != 2.0 || c["flow"] != "xtls-rprx-vision" {
		t.Fatalf("paid client: %v", c)
	}

	// 5. He uses 12 GB and pays again: renewed, usage reset, 60 days left.
	panel.SetUsage("vpn_104", 2*xui.GB, 10*xui.GB)
	sc.step("۵. مهدی: تمدید با پرداخت دوباره", func() {
		sim.Paid(mehdi, "vpn30:104", "XTR", 100)
	})
	sc.lastBot(mehdi, "اشتراک شما تمدید شد")
	c = stored("vpn_104")
	if left := (c["expiryTime"].(float64) - float64(time.Now().UnixMilli())) / float64(xui.DayMs); left < 59.9 || left > 60.1 {
		t.Fatalf("renewed client should have 60 days, has %.2f", left)
	}
	if st, _ := panel.Client("vpn_104"); st == nil {
		t.Fatal("renewed client vanished")
	}

	// 6. Neda burns through her trial: listed as inactive.
	sc.step("۶. ندا: حجم تست تمام می‌شود", func() {
		sim.Send(neda, neda, "/start")
		sc.wait()
		m := sc.lastBot(neda, "فروشگاه")
		sc.press(neda, m, "🎁 اکانت تست رایگان")
		sc.wait()
		panel.SetUsage("trial_105", xui.GB/2, xui.GB)
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
	c = stored("trial_105")
	if c["totalGB"] != float64(6*xui.GB) || c["enable"] != true {
		t.Fatalf("admin renew: %v", c)
	}
	if c, _ := panel.Client("trial_103"); c != nil {
		t.Fatal("trial_103 should be deleted")
	}

	// 8. The panel restarts (sessions lost): the bot logs in again.
	logins := panel.Logins
	panel.ExpireSessions()
	sc.step("۸. ری‌استارت پنل: ورود دوباره خودکار", func() {
		sc.press(sara, menu, "📊 اکانت‌های من")
	})
	sc.lastBot(sara, "trial_102")
	if panel.Logins != logins+1 {
		t.Fatalf("logins %d → %d, want one more", logins, panel.Logins)
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
	_ = engine.Main
}
