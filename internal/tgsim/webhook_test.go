package tgsim_test

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mrjvadi/tgcreator/internal/engine"
	"github.com/mrjvadi/tgcreator/internal/tg"
	"github.com/mrjvadi/tgcreator/internal/tgsim"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// examples/menu-bot in webhook mode: Telegram pushes updates to the bot.
func TestWebhookMenuBot(t *testing.T) {
	sim := tgsim.New("menu_bot")
	defer sim.Close()
	sim.AddUser(201, "Kian", "kian")

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	l.Close()

	wf, err := workflow.Load("../../examples/menu-bot/workflow.json")
	if err != nil {
		t.Fatal(err)
	}
	wf.Bot.Mode = "webhook"
	wf.Bot.Webhook = workflow.Webhook{
		URL: "http://127.0.0.1:" + port + "/hook", Listen: "127.0.0.1:" + port, Path: "/hook", SecretToken: "s3cret",
	}
	e, err := engine.New(wf, engine.Options{Client: tg.New(sim.Token, sim.URL), OnUpdateHandled: sim.Handled})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()

	// Wait for the server and setWebhook.
	for i := 0; i < 100 && len(sim.CallsTo("setWebhook")) == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	set := sim.CallsTo("setWebhook")
	if len(set) != 1 || set[0].Params["secret_token"] != "s3cret" {
		t.Fatalf("setWebhook calls: %#v", set)
	}

	// A forged request without the secret is refused.
	resp, err := http.Post("http://127.0.0.1:"+port+"/hook", "application/json", strings.NewReader(`{"update_id":1,"message":{"text":"/start"}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("forged update: status %d, want 403", resp.StatusCode)
	}

	sim.Send(201, 201, "/start")
	if err := sim.WaitIdle(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	menu := sim.LastBot(201)
	if menu == nil || !strings.Contains(menu.Text, "سلام Kian") {
		t.Fatalf("menu = %#v", menu)
	}
	if err := sim.Press(201, menu, "✍️ ارسال نظر"); err != nil {
		t.Fatal(err)
	}
	if err := sim.WaitIdle(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	sim.Send(201, 201, "خیلی خوب بود")
	if err := sim.WaitIdle(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if last := sim.LastBot(201); !strings.Contains(last.Text, "خیلی خوب بود") {
		t.Fatalf("feedback reply = %q", last.Text)
	}
	if len(sim.Violations) > 0 || len(sim.APIErrors()) > 0 {
		t.Fatalf("violations=%v errors=%v", sim.Violations, sim.APIErrors())
	}
}
