package tgsim_test

// End-to-end scenario: examples/uploader-bot stores files the admin sends
// and hands them out by code or link, with forced channel membership,
// auto-delete and admin commands, on in-memory Redis.
//
//	TGC_SIM_TRANSCRIPT=/tmp/uploader go test ./internal/tgsim/ -run Uploader -v

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/mrjvadi/tgcreator/internal/engine"
	"github.com/mrjvadi/tgcreator/internal/tg"
	"github.com/mrjvadi/tgcreator/internal/tgsim"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

const uploads = -1004004 // the channel users must join

func TestScenarioUploader(t *testing.T) {
	mr := miniredis.RunT(t)
	t.Setenv("TGC_REDIS_URL", "redis://"+mr.Addr())

	sim := tgsim.New("file_box_bot")
	defer sim.Close()
	for _, u := range []struct {
		id         int64
		name, nick string
	}{{ali, "Ali", "ali_dev"}, {sara, "Sara", "sara_go"}, {reza, "Reza", ""}, {mehdi, "Mehdi", "mehdi"}, {neda, "Neda", "neda"}, {omid, "Omid", "omid"}} {
		sim.AddUser(u.id, u.name, u.nick)
	}
	sim.AddChat(uploads, "channel", "File Box", "file_box")
	sim.SetMember(uploads, ali, "creator")
	sim.SetMember(uploads, sim.BotID(), "administrator", tgsim.AdminRights...)
	sim.SetMember(uploads, reza, "member")
	sim.SetMember(uploads, neda, "member")

	wf, err := workflow.Load("../../examples/uploader-bot/workflow.json")
	if err != nil {
		t.Fatal(err)
	}
	wf.Variables["admins"] = []any{float64(ali)}
	wf.Variables["channel"] = "@file_box"
	wf.Variables["auto_delete"] = "200ms"
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
			_ = os.WriteFile(prefix+".md", []byte("# Uploader bot transcript\n"+sim.Transcript()), 0o644)
			_ = os.WriteFile(prefix+".html", []byte(sim.HTMLTranscript("سناریوی ربات آپلودر")), 0o644)
		}
	}()
	sc := &scenario{t: t, sim: sim}
	time.Sleep(50 * time.Millisecond)

	codeRe := regexp.MustCompile(`کد: ([A-Za-z0-9]{8})`)
	upload := func(kind, caption, name string) (string, *tgsim.Msg) {
		t.Helper()
		sim.SendFile(ali, ali, kind, caption, name)
		sc.wait()
		m := sc.lastBot(ali, "فایل ذخیره شد")
		c := codeRe.FindStringSubmatch(m.Text)
		if c == nil {
			t.Fatalf("no code in %q", m.Text)
		}
		if !strings.Contains(m.Text, "https://t.me/file_box_bot?start="+c[1]) {
			t.Fatalf("no link in %q", m.Text)
		}
		return c[1], m
	}
	delivered := func(chat int64, kind string) *tgsim.Msg {
		t.Helper()
		for _, m := range sim.BotMessages(chat) {
			if m.Media == kind { // delivered, maybe auto-deleted since
				return m
			}
		}
		t.Fatalf("chat %d: no %s delivered", chat, kind)
		return nil
	}

	// 1. The admin uploads a document with a caption; the caption looks
	// like a code but must not be treated as one.
	var docCode string
	sc.step("۱. علی (مدیر): آپلود فایل", func() {
		sim.Send(ali, ali, "/start")
		sc.wait()
		sc.lastBot(ali, "سلام مدیر")
		docCode, _ = upload("document", "Report25", "report.pdf")
	})
	if m := sc.lastBot(ali, "report.pdf"); strings.Contains(m.Text, "معتبر نیست") {
		t.Fatal("the caption was taken for a code")
	}

	// 2. Sara is not in the channel: she must join first.
	sc.step("۲. سارا: لینک بدون عضویت ← عضویت ← فایل", func() {
		sim.Send(sara, sara, "/start "+docCode)
		sc.wait()
		join := sc.lastBot(sara, "ابتدا در کانال ما عضو شوید")
		sc.press(sara, join, "✅ عضو شدم")
		sc.wait()
		if calls := sim.CallsTo("answerCallbackQuery"); len(calls) == 0 || calls[len(calls)-1].Params["show_alert"] != true {
			t.Fatal("pressing before joining must show an alert")
		}
		sim.SetMember(uploads, sara, "member")
		sc.press(sara, join, "✅ عضو شدم")
	})
	doc := delivered(sara, "document")
	if doc.Caption != "Report25" {
		t.Fatalf("caption %q", doc.Caption)
	}
	if send := sim.CallsTo("sendDocument"); len(send) != 1 || send[0].Params["document"] != "SIM_DOCUMENT_1" || send[0].Params["parse_mode"] != nil {
		t.Fatalf("sendDocument: %+v", send)
	}

	// 3. Reza (a member) just sends the code as text.
	sc.step("۳. رضا: ارسال کد به‌صورت متن", func() {
		sim.Send(reza, reza, "  "+docCode+" ")
	})
	delivered(reza, "document")

	// 4. Auto-delete: the file and the warning disappear.
	sc.step("۴. حذف خودکار بعد از ۲۰۰ میلی‌ثانیه", func() {
		sc.lastBot(reza, "حذف می‌شود")
		time.Sleep(400 * time.Millisecond)
	})
	for _, m := range sim.BotMessages(reza) {
		if !m.Deleted && (m.Media == "document" || strings.Contains(m.Text, "حذف می‌شود")) {
			t.Fatalf("message %d should have been deleted", m.ID)
		}
	}

	// 5. A wrong code.
	sc.step("۵. ندا: کد اشتباه", func() {
		sim.Send(neda, neda, "ZZZZ9999")
	})
	sc.lastBot(neda, "معتبر نیست")

	// 6. More uploads: photo and video, each with its own code.
	var photoCode, videoCode string
	var photoMsg *tgsim.Msg
	sc.step("۶. علی: آپلود عکس و ویدیو", func() {
		photoCode, photoMsg = upload("photo", "", "")
		videoCode, _ = upload("video", "کلیپ معرفی", "intro.mp4")
	})
	if strings.Contains(photoMsg.Text, "📄") {
		t.Fatalf("a photo has no file name: %q", photoMsg.Text)
	}
	if photoCode == videoCode || photoCode == docCode {
		t.Fatal("codes must differ")
	}
	sc.step("۷. ندا: دریافت ویدیو", func() {
		sim.Send(neda, neda, "/start "+videoCode)
	})
	if v := delivered(neda, "video"); v.Caption != "کلیپ معرفی" {
		t.Fatalf("video caption %q", v.Caption)
	}

	// 8. Admin commands.
	sc.step("۸. علی: /stats و /info", func() {
		sim.Send(ali, ali, "/stats")
		sc.wait()
		sc.lastBot(ali, "فایل‌های آپلودشده: 3")
		sc.lastBot(ali, "دانلودها: 3")
		sim.Send(ali, ali, "/info "+docCode)
		sc.wait()
		m := sc.lastBot(ali, "report.pdf")
		if !strings.Contains(m.Text, "دانلود: 2") {
			t.Fatalf("info: %q", m.Text)
		}
	})

	// 9. Delete with the button, then with /del.
	sc.step("۹. حذف با دکمه و با /del", func() {
		sc.press(ali, photoMsg, "🗑 حذف این فایل")
		sc.wait()
		if m := sim.Snapshot(ali).Messages; !strings.Contains(textOf(m, photoMsg.ID), "حذف شد") {
			t.Fatalf("the upload message should say the file is deleted: %q", textOf(m, photoMsg.ID))
		}
		sim.Send(omid, omid, "/start "+photoCode)
		sc.wait()
		sim.SetMember(uploads, omid, "member")
		sim.Send(omid, omid, "/start "+photoCode)
		sc.wait()
		sc.lastBot(omid, "معتبر نیست")
		sim.Send(ali, ali, "/del "+videoCode)
		sc.wait()
		sc.lastBot(ali, "حذف شد")
		sim.Send(ali, ali, "/del "+videoCode)
	})
	sc.lastBot(ali, "معتبر نیست")

	// 10. Only admins can upload.
	sc.step("۱۰. امید: آپلود توسط غیرمدیر", func() {
		sim.SendFile(omid, omid, "document", "", "virus.exe")
	})
	if m := sim.LastBot(omid); m != nil && strings.Contains(m.Text, "ذخیره شد") {
		t.Fatal("a non-admin must not store files")
	}
	if keys := mr.Keys(); len(keys) == 0 {
		t.Fatal("nothing stored in Redis")
	}

	if errs := sim.APIErrors(); len(errs) > 0 {
		t.Fatalf("Bot API errors: %+v", errs)
	}
}

func textOf(msgs []tgsim.MsgView, id int64) string {
	for _, m := range msgs {
		if m.ID == id {
			return m.Text
		}
	}
	return ""
}
