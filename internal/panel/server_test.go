package panel

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	_ "github.com/mrjvadi/tgcreator/internal/nodes"
	"github.com/mrjvadi/tgcreator/internal/tgsim"
	"github.com/mrjvadi/tgcreator/internal/vpn/vpnfake"
)

func call(t *testing.T, srv http.Handler, method, url string, body any, out any) int {
	t.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		r = bytes.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		r = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, url, r)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: %v: %s", method, url, err, rec.Body.String())
		}
	}
	return rec.Code
}

func TestPanelAPI(t *testing.T) {
	s := New(Config{}, nil)
	defer s.Close()

	var cat struct {
		Nodes []struct {
			Name string `json:"name"`
			Meta struct {
				Label string `json:"label"`
			} `json:"meta"`
		} `json:"nodes"`
		BotAPI string `json:"bot_api"`
	}
	if call(t, s, "GET", "/api/catalog", nil, &cat); len(cat.Nodes) < 30 || cat.BotAPI == "" {
		t.Fatalf("catalog: %d nodes, %q", len(cat.Nodes), cat.BotAPI)
	}
	var methods struct {
		Methods []struct{ Name string } `json:"methods"`
	}
	if call(t, s, "GET", "/api/botapi/methods", nil, &methods); len(methods.Methods) < 180 {
		t.Fatalf("bot api methods: %d", len(methods.Methods))
	}

	var list []struct{ ID string }
	call(t, s, "GET", "/api/examples", nil, &list)
	if len(list) != 6 {
		t.Fatalf("examples: %+v", list)
	}
	req := httptest.NewRequest("GET", "/api/examples/menu-bot", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	menuBot := rec.Body.Bytes()

	var v struct {
		OK      bool     `json:"ok"`
		Updates []string `json:"updates"`
		Issues  []struct{ Level, Node, Message string }
	}
	call(t, s, "POST", "/api/validate", menuBot, &v)
	if !v.OK || len(v.Updates) != 2 {
		t.Fatalf("validate menu-bot: %+v", v)
	}
	call(t, s, "POST", "/api/validate", []byte(`{"nodes":[{"id":"a","type":"telegram.send_message"}]}`), &v)
	if v.OK || len(v.Issues) < 2 {
		t.Fatalf("broken workflow must report issues: %+v", v)
	}

	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("POST", "/api/compose", bytes.NewReader(menuBot)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "TGC_BOT_TOKEN") || strings.Contains(rec.Body.String(), "redis:") {
		t.Fatalf("compose: %d %s", rec.Code, rec.Body.String())
	}

	// Live test session: /start, then press a button.
	var sess struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	if code := call(t, s, "POST", "/api/test", menuBot, &sess); code != 200 {
		t.Fatalf("start test: %d %s", code, sess.Error)
	}
	var res struct {
		Chat tgsim.ChatSnapshot `json:"chat"`
	}
	call(t, s, "POST", "/api/test/"+sess.ID+"/action", map[string]any{"type": "send", "chat_id": UserMe, "text": "/start"}, &res)
	msgs := res.Chat.Messages
	if len(msgs) != 2 || !msgs[1].ByBot || !strings.Contains(msgs[1].Text, "سلام شما") || len(msgs[1].Buttons) == 0 {
		t.Fatalf("after /start: %+v", msgs)
	}
	call(t, s, "POST", "/api/test/"+sess.ID+"/action", map[string]any{"type": "press", "chat_id": UserMe, "message_id": msgs[1].ID, "button": "✍️ ارسال نظر"}, &res)
	if last := res.Chat.Messages[len(res.Chat.Messages)-1]; !strings.Contains(last.Text, "نظرت را بنویس") {
		t.Fatalf("after press: %+v", res.Chat.Messages)
	}
	var evs struct{ Events []tgsim.Event }
	call(t, s, "GET", "/api/test/"+sess.ID+"/events?after=0", nil, &evs)
	if len(evs.Events) < 4 {
		t.Fatalf("events: %+v", evs.Events)
	}
	call(t, s, "DELETE", "/api/test/"+sess.ID, nil, nil)
	if code := call(t, s, "GET", "/api/test/"+sess.ID+"/events", nil, nil); code != 404 {
		t.Fatalf("stopped session still answers: %d", code)
	}
}

func TestPanelTestSessionWithRedis(t *testing.T) {
	s := New(Config{}, nil)
	defer s.Close()
	req := httptest.NewRequest("GET", "/api/examples/anti-link", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	// anti-link also needs Postgres: without a test DSN it must fail clearly.
	var sess struct{ ID, Error string }
	code := call(t, s, "POST", "/api/test", rec.Body.Bytes(), &sess)
	if code == 200 {
		call(t, s, "DELETE", "/api/test/"+sess.ID, nil, nil)
		t.Skip("a Postgres is reachable; nothing to assert")
	}
	if !strings.Contains(sess.Error, "TGC_TEST_DB_MAIN_DSN") {
		t.Fatalf("want a clear service error, got %d %q", code, sess.Error)
	}
}

func TestPanelBasicAuth(t *testing.T) {
	s := New(Config{Password: "p4ss"}, nil)
	defer s.Close()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/api/catalog", nil))
	if rec.Code != 401 {
		t.Fatalf("no auth: %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/api/catalog", nil)
	req.SetBasicAuth("admin", "p4ss")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("with auth: %d", rec.Code)
	}
}

// Buttons connected straight to the next node, through the panel test chat.
func TestPanelButtonMenu(t *testing.T) {
	s := New(Config{}, nil)
	defer s.Close()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/api/examples/button-menu", nil))
	var sess struct{ ID, Error string }
	if code := call(t, s, "POST", "/api/test", rec.Body.Bytes(), &sess); code != 200 {
		t.Fatalf("start: %d %s", code, sess.Error)
	}
	defer call(t, s, "DELETE", "/api/test/"+sess.ID, nil, nil)
	act := func(a map[string]any) tgsim.ChatSnapshot {
		var res struct{ Chat tgsim.ChatSnapshot }
		a["chat_id"] = UserMe
		if code := call(t, s, "POST", "/api/test/"+sess.ID+"/action", a, &res); code != 200 {
			t.Fatalf("%v: %d", a, code)
		}
		return res.Chat
	}
	last := func(c tgsim.ChatSnapshot) tgsim.MsgView { return c.Messages[len(c.Messages)-1] }

	menu := last(act(map[string]any{"type": "send", "text": "/start"}))
	if !strings.Contains(menu.Text, "چه کاری") || len(menu.Buttons) != 2 {
		t.Fatalf("menu = %+v", menu)
	}
	steps := []struct{ button, want string }{
		{"📦 محصولات", "محصولات ما"},
		{"🔙 بازگشت", "چه کاری برایتان"},
		{"ℹ️ درباره ما", "ربات‌های تلگرامی"},
		{"🔙 بازگشت", "چه کاری برایتان"},
	}
	for _, st := range steps {
		c := act(map[string]any{"type": "press", "message_id": menu.ID, "button": st.button})
		if got := last(c); got.ID != menu.ID || !strings.Contains(got.Text, st.want) {
			t.Fatalf("after %q: %+v (the same message must be edited)", st.button, got)
		}
	}
	var evs struct{ Events []tgsim.Event }
	call(t, s, "GET", "/api/test/"+sess.ID+"/events?after=0", nil, &evs)
	for _, e := range evs.Events {
		if e.Kind == "error" || e.Kind == "log" {
			t.Errorf("unexpected problem: %s", e.Text)
		}
	}
}

// The VPN shop runs against an in-memory panel of the configured type:
// the real one in the workflow is never contacted.
func TestPanelVPNSession(t *testing.T) {
	rec := httptest.NewRecorder()
	New(Config{}, nil).ServeHTTP(rec, httptest.NewRequest("GET", "/api/examples/vpn-shop", nil))
	var wf map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &wf); err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"3x-ui", "marzban", "remnawave", "hiddify"} {
		t.Run(typ, func(t *testing.T) {
			s := New(Config{}, nil)
			defer s.Close()
			wf["services"].(map[string]any)["vpn"].(map[string]any)["main"].(map[string]any)["type"] = typ
			if typ != "3x-ui" {
				wf["variables"].(map[string]any)["inbound"] = ""
			}
			body, _ := json.Marshal(wf)
			var sess struct{ ID, Error string }
			if code := call(t, s, "POST", "/api/test", body, &sess); code != 200 {
				t.Fatalf("start: %d %s", code, sess.Error)
			}
			defer call(t, s, "DELETE", "/api/test/"+sess.ID, nil, nil)
			act := func(a map[string]any) tgsim.MsgView {
				var res struct{ Chat tgsim.ChatSnapshot }
				a["chat_id"] = UserMe
				if code := call(t, s, "POST", "/api/test/"+sess.ID+"/action", a, &res); code != 200 {
					t.Fatalf("%v: %d", a, code)
				}
				return res.Chat.Messages[len(res.Chat.Messages)-1]
			}
			menu := act(map[string]any{"type": "send", "text": "/start"})
			got := act(map[string]any{"type": "press", "message_id": menu.ID, "button": "🎁 اکانت تست رایگان"})
			if !strings.Contains(got.Text, "اکانت تست شما آماده است") || !strings.Contains(got.Text, "http") {
				t.Fatalf("trial = %q", got.Text)
			}
			if typ == "3x-ui" && !strings.Contains(got.Text, "@panel.example.com:443") {
				t.Fatalf("config link should use the configured panel host: %q", got.Text)
			}
			if got = act(map[string]any{"type": "press", "message_id": menu.ID, "button": "📊 اکانت‌های من"}); !strings.Contains(got.Text, "trial_1001") {
				t.Fatalf("accounts = %q", got.Text)
			}
		})
	}
}

// The settings dialog's connection test reports success and failure.
func TestPanelVPNCheck(t *testing.T) {
	s := New(Config{}, nil)
	defer s.Close()
	fake := vpnfake.New("pasarguard")
	defer fake.Close()
	var res struct {
		OK     bool
		Error  string
		Groups []struct{ ID, Name string }
	}
	call(t, s, "POST", "/api/vpn/check", fake.Config(), &res)
	if !res.OK || len(res.Groups) != 2 || res.Groups[0].Name != "Main" {
		t.Fatalf("check: %+v", res)
	}
	bad := fake.Config()
	bad.Password = "wrong"
	call(t, s, "POST", "/api/vpn/check", bad, &res)
	if res.OK || !strings.Contains(res.Error, "wrong username or password") {
		t.Fatalf("bad password: %+v", res)
	}
}

// The test chat can send files: the uploader example stores one for the
// admin (the test user) and asks the second user to join the channel.
func TestPanelUploaderSession(t *testing.T) {
	s := New(Config{}, nil)
	defer s.Close()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/api/examples/uploader-bot", nil))
	var sess struct{ ID, Error string }
	if code := call(t, s, "POST", "/api/test", rec.Body.Bytes(), &sess); code != 200 {
		t.Fatalf("start: %d %s", code, sess.Error)
	}
	defer call(t, s, "DELETE", "/api/test/"+sess.ID, nil, nil)
	act := func(a map[string]any) tgsim.MsgView {
		var res struct{ Chat tgsim.ChatSnapshot }
		if code := call(t, s, "POST", "/api/test/"+sess.ID+"/action", a, &res); code != 200 {
			t.Fatalf("%v: %d", a, code)
		}
		return res.Chat.Messages[len(res.Chat.Messages)-1]
	}
	got := act(map[string]any{"type": "file", "chat_id": UserMe, "kind": "document", "name": "book.pdf", "text": "کتاب"})
	m := regexp.MustCompile(`start=([A-Za-z0-9]{8})`).FindStringSubmatch(got.Text)
	if m == nil || !strings.Contains(got.Text, "book.pdf") {
		t.Fatalf("upload reply: %q", got.Text)
	}
	if got = act(map[string]any{"type": "send", "chat_id": UserOther, "user_id": UserOther, "text": "/start " + m[1]}); !strings.Contains(got.Text, "عضو شوید") {
		t.Fatalf("second user: %q", got.Text)
	}
	if got = act(map[string]any{"type": "send", "chat_id": UserMe, "text": m[1]}); got.Media != "document" || got.Caption != "کتاب" {
		t.Fatalf("admin gets the file: %+v", got)
	}
	var res struct{ Error string }
	if code := call(t, s, "POST", "/api/test/"+sess.ID+"/action", map[string]any{"type": "file", "chat_id": UserMe, "kind": "exe"}, &res); code != 400 {
		t.Fatalf("bad kind: %d", code)
	}
}
