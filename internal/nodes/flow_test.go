package nodes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mrjvadi/tgcreator/internal/engine"
	"github.com/mrjvadi/tgcreator/internal/tg"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

type call struct {
	Method string
	Params map[string]any
}

// fakeTelegram records calls and answers like the Bot API.
type fakeTelegram struct {
	mu    sync.Mutex
	calls []call
}

func (f *fakeTelegram) handler(w http.ResponseWriter, r *http.Request) {
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	var p map[string]any
	_ = json.NewDecoder(r.Body).Decode(&p)
	f.mu.Lock()
	f.calls = append(f.calls, call{method, p})
	f.mu.Unlock()

	var result any = true
	switch method {
	case "sendMessage", "sendPhoto", "editMessageText":
		result = map[string]any{"message_id": 777, "chat": map[string]any{"id": p["chat_id"]}}
	case "getChatMember":
		status := "member"
		if p["user_id"] == 1.0 {
			status = "administrator"
		}
		result = map[string]any{"status": status}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

func (f *fakeTelegram) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.Method
	}
	return out
}

func setup(t *testing.T, wfJSON string) (*engine.Engine, *fakeTelegram) {
	t.Helper()
	resetAdminCache()
	fake := &fakeTelegram{}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(srv.Close)
	wf, err := workflow.Parse([]byte(wfJSON))
	if err != nil {
		t.Fatal(err)
	}
	e, err := engine.New(wf, engine.Options{Client: tg.New("TEST", srv.URL)})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.SetBotInfo(map[string]any{"username": "my_bot"})
	return e, fake
}

func msgUpdate(chatType string, userID float64, text string) map[string]any {
	return map[string]any{
		"update_id": 1.0,
		"message": map[string]any{
			"message_id": 10.0,
			"chat":       map[string]any{"id": -1001234567890.0, "type": chatType},
			"from":       map[string]any{"id": userID, "first_name": "Sara"},
			"text":       text,
		},
	}
}

func TestCommandFlow(t *testing.T) {
	e, fake := setup(t, `{
	  "name": "t",
	  "nodes": [
	    {"id": "start", "type": "trigger.command", "params": {"commands": ["start"]}},
	    {"id": "hello", "type": "telegram.send_message", "params": {
	      "text": "hi {{ from.first_name }} {{ args_text }}",
	      "buttons": [[{"text": "A", "callback_data": "a"}], {"text": "{{ '' }}", "callback_data": "hidden"}],
	      "reply": true
	    }},
	    {"id": "log", "type": "logic.set", "params": {"vars": {"sent": "{{ nodes.hello.message_id }}"}}},
	    {"id": "check", "type": "logic.if", "params": {"condition": "vars.sent == 777"}},
	    {"id": "yes", "type": "tg.pinChatMessage", "params": {"chat_id": "{{ chat.id }}", "message_id": "{{ vars.sent }}"}}
	  ],
	  "connections": {
	    "start": {"main": ["hello"]},
	    "hello": {"main": ["log"]},
	    "log": {"main": ["check"]},
	    "check": {"true": ["yes"]}
	  }
	}`)

	e.HandleUpdate(context.Background(), msgUpdate("private", 5, "/start@my_bot  ref42"))
	e.HandleUpdate(context.Background(), msgUpdate("private", 5, "/start@other_bot"))

	if got := strings.Join(fake.methods(), ","); got != "sendMessage,pinChatMessage" {
		t.Fatalf("calls = %s", got)
	}
	p := fake.calls[0].Params
	if p["text"] != "hi Sara ref42" || p["chat_id"] != -1001234567890.0 {
		t.Errorf("params = %#v", p)
	}
	kb := p["reply_markup"].(map[string]any)["inline_keyboard"].([]any)
	if len(kb) != 1 {
		t.Errorf("empty buttons should be dropped: %#v", kb)
	}
	if p["reply_parameters"].(map[string]any)["message_id"] != 10.0 {
		t.Errorf("reply_parameters = %#v", p["reply_parameters"])
	}
	if fake.calls[1].Params["message_id"] != 777.0 {
		t.Errorf("pin params = %#v", fake.calls[1].Params)
	}
}

func TestAdminAndStateFlow(t *testing.T) {
	e, fake := setup(t, `{
	  "name": "t",
	  "nodes": [
	    {"id": "m", "type": "trigger.message", "params": {"chat_types": ["supergroup"], "condition": "hasLink(message)"}},
	    {"id": "adm", "type": "telegram.check_admin"},
	    {"id": "del", "type": "telegram.delete_message"},
	    {"id": "remember", "type": "state.set", "params": {"values": {"warned": "{{ (state.warned ?? 0) + 1 }}"}}},
	    {"id": "second", "type": "trigger.message", "params": {"condition": "state.warned >= 2"}},
	    {"id": "ban", "type": "tg.banChatMember", "params": {"chat_id": "{{ chat.id }}", "user_id": "{{ from.id }}"}}
	  ],
	  "connections": {
	    "m": {"main": ["adm"]},
	    "adm": {"false": ["del"]},
	    "del": {"main": ["remember"]},
	    "second": {"main": ["ban"]}
	  }
	}`)
	ctx := context.Background()
	e.HandleUpdate(ctx, msgUpdate("supergroup", 1, "admin link https://a.com")) // admin: ignored
	e.HandleUpdate(ctx, msgUpdate("supergroup", 2, "no link here"))             // no link
	e.HandleUpdate(ctx, msgUpdate("supergroup", 2, "visit t.me/spam"))          // warn #1
	e.HandleUpdate(ctx, msgUpdate("supergroup", 2, "again www.spam.com"))       // warn #2 -> ban

	got := strings.Join(fake.methods(), ",")
	want := "getChatMember,getChatMember,deleteMessage,deleteMessage,banChatMember"
	if got != want {
		t.Fatalf("calls = %s\nwant    %s", got, want)
	}
}

func TestErrorOutput(t *testing.T) {
	e, fake := setup(t, `{
	  "name": "t",
	  "nodes": [
	    {"id": "c", "type": "trigger.callback", "params": {"prefix": "buy:"}},
	    {"id": "bad", "type": "http.request", "params": {"url": "http://127.0.0.1:1/nope", "timeout": "1s"}},
	    {"id": "oops", "type": "telegram.answer_callback", "params": {"text": "failed: {{ error.node }}", "show_alert": true}}
	  ],
	  "connections": {"c": {"main": ["bad"]}, "bad": {"error": ["oops"]}}
	}`)
	e.HandleUpdate(context.Background(), map[string]any{
		"update_id": 2.0,
		"callback_query": map[string]any{
			"id": "cb1", "data": "buy:9",
			"from":    map[string]any{"id": 3.0},
			"message": map[string]any{"message_id": 4.0, "chat": map[string]any{"id": 3.0, "type": "private"}},
		},
	})
	if len(fake.calls) != 1 || fake.calls[0].Params["callback_query_id"] != "cb1" || fake.calls[0].Params["text"] != "failed: bad" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestRequirementsOnlyWhatIsUsed(t *testing.T) {
	wf, err := workflow.Parse([]byte(`{
	  "name": "t",
	  "services": {"databases": {"logs": {"driver": "postgres", "dsn": "x"}, "unused": {"driver": "mysql", "dsn": "y"}}},
	  "nodes": [
	    {"id": "t", "type": "trigger.update", "params": {"on": ["chat_member"]}},
	    {"id": "q", "type": "db.exec", "params": {"db": "logs", "query": "SELECT 1"}},
	    {"id": "r", "type": "redis.incr", "params": {"key": "x"}}
	  ]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	e, err := engine.New(wf, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(e.Requirements(), ","); got != "db:logs,redis" {
		t.Errorf("requirements = %s", got)
	}
	if got := strings.Join(e.AllowedUpdates(), ","); got != "chat_member" {
		t.Errorf("allowed updates = %s", got)
	}
}

func TestUnknownNodeType(t *testing.T) {
	wf, _ := workflow.Parse([]byte(`{"name":"t","nodes":[{"id":"a","type":"nope.x"}]}`))
	if _, err := engine.New(wf, engine.Options{}); err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("err = %v", err)
	}
}

func resetAdminCache() {
	adminCache.Lock()
	adminCache.m = map[string]adminEntry{}
	adminCache.Unlock()
}

func TestRunPolling(t *testing.T) {
	var mu sync.Mutex
	var offsets []any
	sent := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		var result any = true
		switch method {
		case "getMe":
			result = map[string]any{"id": 1, "username": "my_bot"}
		case "getUpdates":
			mu.Lock()
			offsets = append(offsets, p["offset"])
			first := len(offsets) == 1
			mu.Unlock()
			if first {
				result = []any{msgUpdate("private", 5, "/start")}
			} else {
				<-r.Context().Done()
				return
			}
		case "sendMessage":
			sent <- p
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
	}))
	defer srv.Close()

	wf, _ := workflow.Parse([]byte(`{"name":"t","nodes":[
	  {"id":"s","type":"trigger.command","params":{"commands":["start"]}},
	  {"id":"m","type":"telegram.send_message","params":{"text":"ok"}}],
	  "connections":{"s":{"main":["m"]}}}`))
	e, err := engine.New(wf, engine.Options{Client: tg.New("T", srv.URL)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	select {
	case p := <-sent:
		if p["text"] != "ok" {
			t.Errorf("sent %#v", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no message sent")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(offsets) < 2 || offsets[1] != 2.0 {
		t.Errorf("offsets = %v (second poll must ack update_id 1)", offsets)
	}
}

// A logic.delay must not stall the worker: the next update of the same
// chat is answered while the delayed branch is still waiting.
func TestDelayDoesNotBlockChat(t *testing.T) {
	var mu sync.Mutex
	var texts []string
	var polled int
	sent := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		var result any = true
		switch method {
		case "getMe":
			result = map[string]any{"id": 1, "username": "b"}
		case "getUpdates":
			mu.Lock()
			polled++
			first := polled == 1
			mu.Unlock()
			if !first {
				<-r.Context().Done()
				return
			}
			slow, fast := msgUpdate("private", 5, "slow"), msgUpdate("private", 5, "fast")
			fast["update_id"] = 2.0
			result = []any{slow, fast}
		case "sendMessage":
			mu.Lock()
			texts = append(texts, p["text"].(string))
			mu.Unlock()
			sent <- p["text"].(string)
			result = map[string]any{"message_id": 1}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
	}))
	defer srv.Close()

	wf, _ := workflow.Parse([]byte(`{"name":"t","runtime":{"workers":1},"nodes":[
	  {"id":"m","type":"trigger.message"},
	  {"id":"is_slow","type":"logic.if","params":{"condition":"text == 'slow'"}},
	  {"id":"wait","type":"logic.delay","params":{"duration":"700ms"}},
	  {"id":"late","type":"telegram.send_message","params":{"text":"late"}},
	  {"id":"quick","type":"telegram.send_message","params":{"text":"fast"}}],
	  "connections":{"m":{"main":["is_slow"]},"is_slow":{"true":["wait"],"false":["quick"]},"wait":{"main":["late"]}}}`))
	handled := make(chan struct{}, 2)
	e, err := engine.New(wf, engine.Options{Client: tg.New("T", srv.URL), OnUpdateHandled: func(map[string]any) { handled <- struct{}{} }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()

	start := time.Now()
	if first := <-sent; first != "fast" || time.Since(start) > 400*time.Millisecond {
		t.Fatalf("first reply %q after %v: the delay blocked the chat", first, time.Since(start))
	}
	if second := <-sent; second != "late" || time.Since(start) < 600*time.Millisecond {
		t.Fatalf("second reply %q after %v", second, time.Since(start))
	}
	<-handled
	<-handled // reported only once the delayed branch finished
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
