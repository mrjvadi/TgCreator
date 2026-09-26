package tgsim_test

// End-to-end scenario: examples/community-bot runs on the real runtime
// (long polling) against the simulator, with real Redis and Postgres.
//
//	TGC_TEST_REDIS_URL=redis://localhost:6379/15 \
//	TGC_TEST_DB_DSN=postgres://.../scenario go test ./internal/tgsim/ -run Scenario -v
//
// The test database's tables and the Redis keys it uses are reset.
// Set TGC_SIM_TRANSCRIPT=/path/prefix to also write .md/.html transcripts.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mrjvadi/tgcreator/internal/engine"
	"github.com/mrjvadi/tgcreator/internal/tg"
	"github.com/mrjvadi/tgcreator/internal/tgsim"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

const (
	ali, sara, reza, mehdi, neda, omid = 101, 102, 103, 104, 105, 106
	gophers                            = -1001001 // supergroup, bot is admin
	family                             = -2002    // basic group, bot is a plain member
	news                               = -1003003 // channel, bot is admin
)

type scenario struct {
	t   *testing.T
	sim *tgsim.Sim
	db  *sql.DB
	rdb *redis.Client
}

func (sc *scenario) step(title string, fn func()) {
	sc.t.Helper()
	sc.sim.Note("## " + title)
	fn()
	if err := sc.sim.WaitIdle(10 * time.Second); err != nil {
		sc.t.Fatalf("%s: %v", title, err)
	}
}

// wait lets queued updates finish before the next action in a step.
func (sc *scenario) wait() {
	sc.t.Helper()
	if err := sc.sim.WaitIdle(10 * time.Second); err != nil {
		sc.t.Fatal(err)
	}
}

func (sc *scenario) lastBot(chat int64, want string) *tgsim.Msg {
	sc.t.Helper()
	m := sc.sim.LastBot(chat)
	if m == nil {
		sc.t.Fatalf("chat %d: bot sent nothing, want %q", chat, want)
	}
	if !strings.Contains(m.Text+m.Caption, want) {
		sc.t.Fatalf("chat %d: last bot message %q, want it to contain %q", chat, m.Text+m.Caption, want)
	}
	return m
}

func (sc *scenario) press(user int64, m *tgsim.Msg, button string) {
	sc.t.Helper()
	if err := sc.sim.Press(user, m, button); err != nil {
		sc.t.Fatal(err)
	}
}

func (sc *scenario) count(query string, args ...any) int {
	sc.t.Helper()
	var n int
	if err := sc.db.QueryRow(query, args...).Scan(&n); err != nil {
		sc.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (sc *scenario) status(chat, user int64, want string) {
	sc.t.Helper()
	if got := sc.sim.Member(chat, user).Status; got != want {
		sc.t.Fatalf("member %d in %d: status %q, want %q", user, chat, got, want)
	}
}

func TestScenarioCommunityBot(t *testing.T) {
	redisURL, dsn := os.Getenv("TGC_TEST_REDIS_URL"), os.Getenv("TGC_TEST_DB_DSN")
	if redisURL == "" || dsn == "" {
		t.Skip("set TGC_TEST_REDIS_URL and TGC_TEST_DB_DSN to run the scenario")
	}
	t.Setenv("TGC_REDIS_URL", redisURL)
	t.Setenv("TGC_DB_MAIN_DSN", dsn)

	// Fresh state.
	if db, err := sql.Open("pgx", dsn); err == nil {
		_, _ = db.Exec("DROP TABLE IF EXISTS users, feedback, warnings, locks, orders")
		db.Close()
	}
	opt, _ := redis.ParseURL(redisURL)
	rc := redis.NewClient(opt)
	for _, pattern := range []string{"flood:*", "lock:*", "poll:*", "tgc:state:*"} {
		keys, _ := rc.Keys(context.Background(), pattern).Result()
		if len(keys) > 0 {
			rc.Del(context.Background(), keys...)
		}
	}
	rc.Close()

	// ---- the world ----
	sim := tgsim.New("gopher_helper_bot")
	defer sim.Close()
	for _, u := range []struct {
		id         int64
		name, nick string
	}{{ali, "Ali", "ali_dev"}, {sara, "Sara", "sara_go"}, {reza, "Reza", ""}, {mehdi, "Mehdi", "mehdi_ads"}, {neda, "Neda", "neda"}, {omid, "Omid", "omid"}} {
		sim.AddUser(u.id, u.name, u.nick)
	}
	sim.AddChat(gophers, "supergroup", "Gophers IR", "gophers_ir")
	sim.SetMember(gophers, ali, "creator")
	sim.SetMember(gophers, sara, "administrator", tgsim.AdminRights...)
	sim.SetMember(gophers, mehdi, "member")
	sim.SetMember(gophers, reza, "member")
	sim.AddChat(family, "group", "Family", "")
	sim.SetMember(family, ali, "creator")
	sim.SetMember(family, reza, "member")
	sim.AddChat(news, "channel", "Gopher News", "gopher_news")
	sim.SetMember(news, ali, "creator")

	// ---- the runtime ----
	wf, err := workflow.Load("../../examples/community-bot/workflow.json")
	if err != nil {
		t.Fatal(err)
	}
	assets, _ := filepath.Abs("../../examples/community-bot/assets")
	wf.Variables["assets"] = assets
	wf.Variables["auto_delete"] = "150ms"
	wf.Runtime.HealthListen = ""

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

	sc := &scenario{t: t, sim: sim, db: e.Service("db:main").(*sql.DB), rdb: e.Service("redis").(*redis.Client)}
	defer func() {
		if prefix := os.Getenv("TGC_SIM_TRANSCRIPT"); prefix != "" {
			_ = os.WriteFile(prefix+".md", []byte("# Scenario transcript\n"+sim.Transcript()), 0o644)
			_ = os.WriteFile(prefix+".html", []byte(sim.HTMLTranscript("سناریوی ربات جامعهٔ گوفرها")), 0o644)
		}
	}()
	time.Sleep(50 * time.Millisecond) // first getUpdates sets allowed_updates

	// ---- 1. setup: the bot is added to chats ----
	sc.step("۱. ربات به گروه‌ها و کانال اضافه می‌شود", func() {
		sim.BotAddedTo(gophers, ali, "administrator", tgsim.AdminRights...)
		sc.wait()
		sim.BotAddedTo(family, ali, "member")
		sc.wait()
		sim.BotAddedTo(news, ali, "administrator", tgsim.AdminRights...)
	})
	sc.lastBot(gophers, "مرا ادمین کنید")
	sc.lastBot(family, "مرا ادمین کنید")
	if sim.LastBot(news) != nil {
		t.Fatal("no greeting expected in the channel")
	}

	// ---- 2. Ali: menu, media, album, referral link ----
	var menu *tgsim.Msg
	sc.step("۲. علی: /start، منو، عکس، آلبوم و لینک دعوت", func() {
		sim.Send(ali, ali, "/start")
		sc.wait()
		menu = sc.lastBot(ali, "سلام Ali")
		sc.press(ali, menu, "📷 عکس خوش‌آمد")
		sc.wait()
		photo := sc.lastBot(ali, "خوش آمدی")
		if photo.Media != "photo" {
			t.Fatalf("expected a photo, got %q", photo.Media)
		}
		sc.press(ali, photo, "🔙 بازگشت به منو")
		sc.wait()
		if !isDeleted(sim, ali, photo.ID) {
			t.Fatal("photo should be deleted when going back to the menu")
		}
		menu = sc.lastBot(ali, "سلام Ali")
		sc.press(ali, menu, "🎞 آلبوم")
		sc.wait()
		sc.press(ali, menu, "🔗 لینک دعوت")
	})
	if n := len(sim.BotMessages(ali)); n != 6 { // menu, photo, menu, 3 album items
		t.Fatalf("Ali's chat: %d bot messages, want 6", n)
	}

	// ---- 3. Sara: referral, feedback, payment, contact ----
	sc.step("۳. سارا با لینک دعوت علی وارد می‌شود و نظر می‌دهد", func() {
		sim.Send(sara, sara, "/start ref_101")
		sc.wait()
		sc.lastBot(ali, "Sara با لینک دعوت شما عضو ربات شد")
		m := sc.lastBot(sara, "سلام Sara")
		sc.press(sara, m, "✍️ ارسال نظر")
		sc.wait()
		sc.lastBot(sara, "نظرت را در یک پیام بنویس")
		sim.Send(sara, sara, "ربات عالیه <3 فقط دکمه‌های بیشتر & تم تیره!")
		sc.wait()
		sc.lastBot(sara, "نظرت ثبت شد")
		sc.lastBot(ali, "ربات عالیه <3 فقط دکمه‌های بیشتر & تم تیره!")
		sim.Send(sara, sara, "سلام دوباره")
	})
	if got := sc.count("SELECT count(*) FROM feedback WHERE user_id = $1", sara); got != 1 {
		t.Fatalf("feedback rows = %d", got)
	}
	if got := sc.count("SELECT count(*) FROM users WHERE user_id = $1 AND referrer = $2", sara, ali); got != 1 {
		t.Fatal("Sara's referrer should be Ali")
	}
	if prompt := sim.BotMessages(sara)[1]; prompt.Markup != nil {
		t.Fatal("feedback prompt buttons should be removed")
	}

	sc.step("۴. سارا اشتراک ویژه می‌خرد (Telegram Stars)", func() {
		// The first menu became the feedback prompt; open a fresh one.
		sim.Send(sara, sara, "/start")
		sc.wait()
		m := sc.lastBot(sara, "سلام Sara")
		sc.press(sara, m, "⭐️ اشتراک ویژه")
		sc.wait()
		inv := sim.LastBot(sara)
		if inv.Media != "invoice" {
			t.Fatalf("expected invoice, got %q", inv.Media)
		}
		bad := sim.PreCheckout(sara, "vip:102", "XTR", 10)
		sc.wait()
		if !sim.Answered(bad) {
			t.Fatal("wrong-amount checkout must be answered (rejected)")
		}
		good := sim.PreCheckout(sara, "vip:102", "XTR", 50)
		sc.wait()
		if !sim.Answered(good) {
			t.Fatal("checkout not answered")
		}
		sim.Paid(sara, "vip:102", "XTR", 50)
		sc.wait()
		sc.lastBot(sara, "پرداخت 50 ⭐️ انجام شد")
	})
	if got := sc.count("SELECT count(*) FROM orders WHERE user_id = $1 AND amount = 50", sara); got != 1 {
		t.Fatalf("orders = %d", got)
	}
	rejections := 0
	for _, c := range sim.CallsTo("answerPreCheckoutQuery") {
		if c.Params["ok"] == false {
			rejections++
		}
	}
	if rejections != 1 {
		t.Fatalf("expected exactly one rejected checkout, got %d", rejections)
	}

	sc.step("۵. سارا شماره‌اش را با دکمهٔ کیبورد می‌فرستد", func() {
		sim.Send(sara, sara, "/contact")
		sc.wait()
		sc.lastBot(sara, "شماره‌ات را")
		sim.SendWith(sara, sara, "", map[string]any{"contact": map[string]any{"phone_number": "+989121234567", "first_name": "Sara", "user_id": float64(sara)}})
	})
	sc.lastBot(sara, "+989121234567 ثبت شد")
	if got := sc.count("SELECT count(*) FROM users WHERE user_id = $1 AND phone = '+989121234567'", sara); got != 1 {
		t.Fatal("phone not saved")
	}

	sc.step("۶. رضا ربات را استارت می‌کند و بعد بلاکش می‌کند", func() {
		sim.Send(reza, reza, "/start")
		sc.wait()
		sim.BotBlockedBy(reza)
	})

	// ---- 7. supergroup ----
	var nedaCaptcha *tgsim.Msg
	sc.step("۷. ندا وارد گروه می‌شود: بی‌صدا + کپچا", func() {
		sim.Join(gophers, neda)
		sc.wait()
		sc.status(gophers, neda, "restricted")
		nedaCaptcha = sc.lastBot(gophers, "Neda به Gophers IR خوش آمدی")
		if sim.Send(gophers, neda, "سلام!") != nil {
			t.Fatal("a restricted user must not be able to write")
		}
		sc.press(mehdi, nedaCaptcha, "✅ من ربات نیستم")
		sc.wait()
		sc.status(gophers, neda, "restricted")
		sc.press(neda, nedaCaptcha, "✅ من ربات نیستم")
	})
	sc.status(gophers, neda, "member")
	if !isDeleted(sim, gophers, nedaCaptcha.ID) {
		t.Fatal("captcha message should be deleted")
	}

	sc.step("۸. قفل لینک: مهدی اجازه ندارد، سارا (ادمین) قفل می‌کند", func() {
		sim.Send(gophers, mehdi, "/lock link")
		sc.wait()
		sc.lastBot(gophers, "فقط ادمین‌ها")
		sim.Send(gophers, sara, "/lock link")
	})
	sc.lastBot(gophers, "قفل شد")
	if v, _ := sc.rdb.Get(context.Background(), "lock:link:-1001001").Result(); v != "1" {
		t.Fatal("lock not in redis")
	}
	if sc.count("SELECT count(*) FROM locks WHERE chat_id = $1 AND set_by = $2", gophers, sara) != 1 {
		t.Fatal("lock not in postgres")
	}

	var hello, spam *tgsim.Msg
	sc.step("۹. مهدی لینک تبلیغاتی می‌فرستد؛ سارا لینک مجاز", func() {
		hello = sim.Send(gophers, mehdi, "سلام به همه")
		sc.wait()
		spam = sim.Send(gophers, mehdi, "عضو کانال ما شوید: t.me/spam_channel")
		sc.wait()
		sim.Send(gophers, sara, "مستندات رسمی: https://go.dev/doc")
		sc.wait()
		sim.Edit(hello, "سلام به همه، اینجا رو ببینید www.cheap-ads.com")
	})
	if !isDeleted(sim, gophers, spam.ID) || !isDeleted(sim, gophers, hello.ID) {
		t.Fatal("spam and edited spam must be deleted")
	}
	if len(sim.CallsTo("deleteMessage")) < 4 { // 2 spam + 2 auto-deleted warnings (+ captcha)
		t.Fatal("warnings should be auto-deleted")
	}

	sc.step("۱۰. ضد اسپم: مهدی پشت‌سرهم پیام می‌دهد و بی‌صدا می‌شود", func() {
		for i := 1; i <= 4; i++ {
			sim.Send(gophers, mehdi, "خرید فالوور ارزان "+strings.Repeat("!", i))
			sc.wait()
		}
	})
	sc.status(gophers, mehdi, "restricted")
	sc.lastBot(gophers, "بی‌صدا شد")
	if sim.Send(gophers, mehdi, "چرا؟") != nil {
		t.Fatal("muted user must not be able to write")
	}

	var rezaMsg *tgsim.Msg
	sc.step("۱۱. اخطار: موارد نامعتبر و سه اخطار به رضا تا اخراج", func() {
		rezaMsg = sim.Send(gophers, reza, "یه سوال داشتم دربارهٔ goroutine ها")
		sc.wait()
		saraMsg := sim.Send(gophers, sara, "بپرس")
		sc.wait()
		sim.Reply(gophers, reza, saraMsg, "/warn")
		sc.wait()
		sc.lastBot(gophers, "فقط ادمین‌ها می‌توانند اخطار بدهند")
		sim.Send(gophers, sara, "/warn")
		sc.wait()
		sc.lastBot(gophers, "ریپلای کن")
		aliMsg := sim.Send(gophers, ali, "من هم هستم")
		sc.wait()
		sim.Reply(gophers, sara, aliMsg, "/warn")
		sc.wait()
		sc.lastBot(gophers, "نمی‌توان به ادمین اخطار داد")
		for i := 1; i <= 3; i++ {
			sim.Reply(gophers, sara, rezaMsg, "/warn")
			sc.wait()
		}
	})
	sc.status(gophers, reza, "kicked")
	sc.lastBot(gophers, "پس از 3 اخطار از گروه اخراج شد")
	if sc.count("SELECT count(*) FROM warnings WHERE user_id = $1", reza) != 0 {
		t.Fatal("warnings should be reset after the ban")
	}

	sc.step("۱۲. سنجاق، نظرسنجی و رأی‌ها", func() {
		ann := sim.Send(gophers, sara, "📣 جلسهٔ ماهانه پنج‌شنبه ساعت ۱۸")
		sc.wait()
		sim.Reply(gophers, sara, ann, "/pin")
		sc.wait()
		sc.lastBot(gophers, "سنجاق شد")
		sim.Send(gophers, ali, "/poll زبان مورد علاقه‌ات برای بک‌اند؟")
		sc.wait()
		poll := sim.LastBot(gophers)
		pollID := sim.PollID(poll)
		if pollID == "" {
			t.Fatal("no poll sent")
		}
		sim.Vote(sara, pollID, 0)
		sc.wait()
		sc.lastBot(gophers, "Sara رأی داد")
		sim.Vote(neda, pollID, 1)
		sc.wait()
		sc.lastBot(gophers, "Neda رأی داد")
		sim.React(gophers, sara, ann, "🔥")
	})

	sc.step("۱۳. امید درخواست عضویت می‌دهد", func() {
		sim.JoinRequest(gophers, omid)
	})
	sc.status(gophers, omid, "member")
	sc.lastBot(omid, "درخواست عضویت شما در «Gophers IR» تأیید شد")

	// ---- basic group where the bot is not admin ----
	sc.step("۱۴. گروه Family: ربات ادمین نیست", func() {
		sim.Send(family, ali, "/lock link")
		sc.wait()
		sc.lastBot(family, "قفل شد")
		sim.Send(family, reza, "این فیلم رو ببینید https://youtu.be/xyz")
		sc.wait()
		sc.lastBot(family, "باید مرا ادمین کنید")
		sim.Join(family, neda)
		sc.wait()
		sc.lastBot(family, "Neda خوش آمدی")
		sc.status(family, neda, "member")
		sim.Send(family, reza, "/dice")
	})
	if d := sim.LastBot(family); d.Media != "dice" {
		t.Fatalf("expected dice, got %q", d.Media)
	}

	// ---- channel and inline mode ----
	var post *tgsim.Msg
	sc.step("۱۵. کانال: پست جدید ← ری‌اکشن و دکمهٔ گفت‌وگو", func() {
		post = sim.ChannelPost(news, "🚀 نسخهٔ جدید Go منتشر شد!")
	})
	if !slices.Contains(post.Reactions, "👍") || post.Markup == nil {
		t.Fatal("channel post should get a reaction and a button")
	}

	sc.step("۱۶. حالت inline: سارا در یک چت دیگر @gopher_helper_bot goroutine تایپ می‌کند", func() {
		q := sim.InlineQuery(sara, "goroutine")
		sc.wait()
		if !sim.Answered(q) {
			t.Fatal("inline query not answered")
		}
	})

	// ---- owner tools ----
	sc.step("۱۷. علی: آمار، ارسال همگانی (رضا بلاک کرده) و آمار دوباره", func() {
		sim.Send(sara, sara, "/stats") // not the owner: ignored
		sc.wait()
		sim.Send(ali, ali, "/stats")
		sc.wait()
		sc.lastBot(ali, "کاربران: 3 (دعوتی: 1)")
		sc.lastBot(ali, "سفارش‌ها: 1 (50 ⭐️)")
		sim.Send(ali, ali, "/broadcast نسخهٔ ۲ ربات منتشر شد 🎉")
		sc.wait()
		sc.lastBot(ali, "✅ 2 موفق، ❌ 1 ناموفق")
		sc.lastBot(sara, "📢 نسخهٔ ۲ ربات منتشر شد 🎉")
		sim.Send(ali, ali, "/stats")
	})
	sc.lastBot(ali, "ربات را بلاک کرده‌اند: 1")
	if sc.count("SELECT count(*) FROM users WHERE user_id = $1 AND blocked", reza) != 1 {
		t.Fatal("Reza should be marked as blocked")
	}

	sc.step("۱۸. مهدی در پیوی /help می‌زند", func() {
		sim.Send(mehdi, mehdi, "/help")
	})
	sc.lastBot(mehdi, "راهنما")

	// ---- global checks ----
	if len(sim.Violations) > 0 {
		t.Errorf("Bot API spec violations:\n%s", strings.Join(sim.Violations, "\n"))
	}
	// Only failures that real Telegram would produce in this story are allowed.
	expected := map[string]bool{
		"sendMessage → Forbidden: bot was blocked by the user":  true, // broadcast to Reza
		"deleteMessage → Bad Request: message can't be deleted": true, // Family: bot not admin
	}
	for _, c := range sim.APIErrors() {
		if key := c.Method + " → " + c.Error; !expected[key] {
			t.Errorf("unexpected Telegram error: %s", key)
		}
	}
	t.Logf("calls: %s", sim.Summary())
}

func isDeleted(sim *tgsim.Sim, chat, id int64) bool {
	for _, c := range sim.CallsTo("deleteMessage") {
		if c.Error == "" && int64(c.Params["chat_id"].(float64)) == chat && int64(c.Params["message_id"].(float64)) == id {
			return true
		}
	}
	return false
}
