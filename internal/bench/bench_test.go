// Package bench measures the runtime: per-update cost, throughput and
// latency under load, with and without simulated Telegram network delay.
//
//	go test -bench . -benchmem ./internal/bench/
//	TGC_LOAD=1 go test -run Load -v ./internal/bench/
//	TGC_TEST_REDIS_URL=... TGC_TEST_DB_DSN=... go test -bench Services ./internal/bench/
package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mrjvadi/tgcreator/internal/engine"
	_ "github.com/mrjvadi/tgcreator/internal/nodes"
	"github.com/mrjvadi/tgcreator/internal/tg"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// memTG is an in-memory Bot API: no sockets, optional artificial latency
// per API call to model the network round trip to Telegram.
type memTG struct {
	latency time.Duration
	batches chan []map[string]any
	calls   atomic.Int64
}

func (m *memTG) RoundTrip(r *http.Request) (*http.Response, error) {
	method := path.Base(r.URL.Path)
	_, _ = io.Copy(io.Discard, r.Body)
	r.Body.Close()
	result := "true"
	switch method {
	case "getMe":
		result = `{"id":999,"is_bot":true,"first_name":"Bench","username":"bench_bot"}`
	case "deleteWebhook":
	case "getUpdates":
		select {
		case b := <-m.batches:
			raw, _ := json.Marshal(b)
			result = string(raw)
		case <-time.After(20 * time.Millisecond):
			result = "[]"
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	default:
		m.calls.Add(1)
		if m.latency > 0 {
			select {
			case <-time.After(m.latency):
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		switch method {
		case "getChatMember":
			result = `{"status":"member","user":{"id":5,"is_bot":false,"first_name":"U"}}`
		case "sendMessage":
			result = `{"message_id":77,"date":0,"chat":{"id":1,"type":"private"},"text":"ok"}`
		}
	}
	return &http.Response{
		StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Request: r,
		Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":` + result + `}`)),
	}, nil
}

func newEngine(tb testing.TB, wfJSON string, m *memTG, onHandled func(map[string]any)) *engine.Engine {
	tb.Helper()
	wf, err := workflow.Parse([]byte(wfJSON))
	if err != nil {
		tb.Fatal(err)
	}
	client := tg.NewWithHTTPClient("T", "http://tg.local", &http.Client{Transport: m})
	e, err := engine.New(wf, engine.Options{Client: client, OnUpdateHandled: onHandled})
	if err != nil {
		tb.Fatal(err)
	}
	if err := e.Open(context.Background()); err != nil {
		tb.Fatal(err)
	}
	e.SetBotInfo(map[string]any{"id": 999.0, "username": "bench_bot"})
	return e
}

// A /start reply with a template and an inline keyboard.
const simpleWF = `{"name":"simple","nodes":[
  {"id":"s","type":"trigger.command","params":{"commands":["start"]}},
  {"id":"r","type":"telegram.send_message","params":{"text":"سلام {{ escapeHTML(from.first_name) }}! شناسه: {{ from.id }}",
    "buttons":[[{"text":"📷 عکس","callback_data":"photo"},{"text":"✍️ نظر","callback_data":"fb"}],[{"text":"🌐 سایت","url":"https://go.dev"}]]}}],
  "connections":{"s":{"main":["r"]}}}`

// Group moderation: several triggers see every message; a link from a
// non-admin is deleted and warned about (admin status is cached).
const moderationWF = `{"name":"moderation","nodes":[
  {"id":"cmd","type":"trigger.command","params":{"commands":["lock","warn","pin","ban"],"chat_types":["group","supergroup"]}},
  {"id":"cmd_ok","type":"logic.log","params":{"message":"cmd"}},
  {"id":"welcome","type":"trigger.message","params":{"has":["new_chat_members"]}},
  {"id":"wl","type":"telegram.send_message","params":{"text":"welcome"}},
  {"id":"link","type":"trigger.message","params":{"chat_types":["group","supergroup"],"condition":"hasLink(message)"}},
  {"id":"adm","type":"telegram.check_admin"},
  {"id":"del","type":"telegram.delete_message"},
  {"id":"warn","type":"telegram.send_message","params":{"text":"🚫 {{ mention(from) }} لینک ممنوع است"}},
  {"id":"words","type":"trigger.message","params":{"chat_types":["group","supergroup"],"condition":"text matches '(?i)(casino|free followers|کازینو)'"}},
  {"id":"del2","type":"telegram.delete_message"}],
  "connections":{"cmd":{"main":["cmd_ok"]},"welcome":{"main":["wl"]},"link":{"main":["adm"]},"adm":{"false":["del"]},"del":{"main":["warn"]},"words":{"main":["del2"]}}}`

// next connects every output of a node (trigger or switch) to target.
func next(from, target string) map[string][]string {
	if from == "t" {
		return map[string][]string{"main": {target}}
	}
	i := strings.TrimPrefix(from, "sw")
	return map[string][]string{"X": {target}, "ALI-" + i: {target}, "default": {target}}
}

// 30 logic nodes (conditions, switches, variables) before one reply.
func logicWF() string {
	var nodes []string
	conns := map[string]map[string][]string{}
	nodes = append(nodes, `{"id":"t","type":"trigger.message"}`)
	prev := "t"
	for i := 0; i < 10; i++ {
		set, cond, sw := fmt.Sprintf("set%d", i), fmt.Sprintf("if%d", i), fmt.Sprintf("sw%d", i)
		nodes = append(nodes,
			fmt.Sprintf(`{"id":%q,"type":"logic.set","params":{"vars":{"n%d":"{{ (vars.n%d ?? 0) + len(text) }}","s":"{{ upper(from.first_name) + '-%d' }}"}}}`, set, i, i, i),
			fmt.Sprintf(`{"id":%q,"type":"logic.if","params":{"condition":"vars.n%d > 0 && chat.type in ['private','group'] && !hasLink(message)"}}`, cond, i),
			fmt.Sprintf(`{"id":%q,"type":"logic.switch","params":{"value":"{{ vars.s }}","cases":["X","ALI-%d"]}}`, sw, i))
		conns[prev] = next(prev, set)
		conns[set] = map[string][]string{"main": {cond}}
		conns[cond] = map[string][]string{"true": {sw}}
		prev = sw
	}
	nodes = append(nodes, `{"id":"reply","type":"telegram.send_message","params":{"text":"{{ vars.s }} {{ vars.n9 }}"}}`)
	conns[prev] = next(prev, "reply")
	c, _ := json.Marshal(conns)
	return `{"name":"logic","nodes":[` + strings.Join(nodes, ",") + `],"connections":` + string(c) + `}`
}

var updateSeq atomic.Int64

func msg(chatID float64, chatType string, userID float64, text string) map[string]any {
	return map[string]any{
		"update_id": float64(updateSeq.Add(1)),
		"message": map[string]any{
			"message_id": 10.0, "date": 0.0, "text": text,
			"chat": map[string]any{"id": chatID, "type": chatType, "title": "Gophers"},
			"from": map[string]any{"id": userID, "is_bot": false, "first_name": "Ali"},
		},
	}
}

func benchHandle(b *testing.B, wfJSON string, mk func(i int) map[string]any) {
	m := &memTG{}
	e := newEngine(b, wfJSON, m, nil)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.HandleUpdate(ctx, mk(i))
	}
	b.ReportMetric(float64(m.calls.Load())/float64(b.N), "api-calls/op")
}

func BenchmarkStartReply(b *testing.B) {
	benchHandle(b, simpleWF, func(i int) map[string]any { return msg(float64(i%1000+1), "private", float64(i%1000+1), "/start") })
}

func BenchmarkGroupMessageNoAction(b *testing.B) {
	benchHandle(b, moderationWF, func(i int) map[string]any {
		return msg(-100, "supergroup", 5, "سلام به همه، کسی Go کار کرده؟")
	})
}

func BenchmarkGroupLinkDeleted(b *testing.B) {
	benchHandle(b, moderationWF, func(i int) map[string]any { return msg(-100, "supergroup", 5, "join t.me/spam_channel now") })
}

func BenchmarkThirtyLogicNodes(b *testing.B) {
	benchHandle(b, logicWF(), func(i int) map[string]any { return msg(1, "private", 1, "hello world") })
}

// ---- load test through the real Run loop ----

type loadResult struct {
	updates  int
	elapsed  time.Duration
	p50, p99 time.Duration
	calls    int64
}

// load pushes n updates spread over chats through Run and measures the time
// from Telegram having each update to the bot having fully handled it.
func load(t *testing.T, wfJSON string, workers, n, chats int, latency time.Duration, mk func(chat float64, i int) map[string]any) loadResult {
	t.Helper()
	wf := strings.Replace(wfJSON, `"name":`, fmt.Sprintf(`"runtime":{"workers":%d},"name":`, workers), 1)
	m := &memTG{latency: latency, batches: make(chan []map[string]any, n/100+2)}

	var mu sync.Mutex
	pushed := make(map[float64]time.Time, n)
	lat := make([]time.Duration, 0, n)
	done := make(chan struct{})
	e := newEngine(t, wf, m, func(u map[string]any) {
		now := time.Now()
		mu.Lock()
		lat = append(lat, now.Sub(pushed[u["update_id"].(float64)]))
		finished := len(lat) == n
		mu.Unlock()
		if finished {
			close(done)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- e.Run(ctx) }()
	time.Sleep(30 * time.Millisecond)

	start := time.Now()
	batch := make([]map[string]any, 0, 100) // Telegram returns up to 100 per getUpdates
	for i := 0; i < n; i++ {
		u := mk(float64(-(1000000 + i%chats)), i)
		mu.Lock()
		pushed[u["update_id"].(float64)] = time.Now()
		mu.Unlock()
		batch = append(batch, u)
		if len(batch) == 100 || i == n-1 {
			m.batches <- batch
			batch = make([]map[string]any, 0, 100)
		}
	}
	select {
	case <-done:
	case <-time.After(5 * time.Minute):
		t.Fatalf("timeout: %d/%d handled", len(lat), n)
	}
	elapsed := time.Since(start)
	cancel()
	if err := <-runErr; err != nil {
		t.Fatal(err)
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	return loadResult{updates: n, elapsed: elapsed, p50: lat[len(lat)/2], p99: lat[len(lat)*99/100], calls: m.calls.Load()}
}

func TestLoad(t *testing.T) {
	if os.Getenv("TGC_LOAD") == "" {
		t.Skip("set TGC_LOAD=1")
	}
	groupMsg := func(chat float64, i int) map[string]any {
		if i%10 == 0 { // 10% of messages contain a link
			return msg(chat, "supergroup", float64(i%500+1), "join t.me/spam")
		}
		return msg(chat, "supergroup", float64(i%500+1), "سلام به همه")
	}
	start := func(chat float64, i int) map[string]any { return msg(-chat, "private", -chat, "/start") }

	cases := []struct {
		name          string
		wf            string
		workers, n, c int
		latency       time.Duration
		mk            func(float64, int) map[string]any
	}{
		{"1 /start, no network", simpleWF, 0, 1, 1, 0, start},
		{"1 /start, 100ms to Telegram", simpleWF, 0, 1, 1, 100 * time.Millisecond, start},
		{"1,000 /start, no network", simpleWF, 0, 1000, 1000, 0, start},
		{"100,000 group msgs, no network", moderationWF, 0, 100000, 1000, 0, groupMsg},
		{"10,000 /start, 100ms, 16 at once", simpleWF, 16, 10000, 1000, 100 * time.Millisecond, start},
		{"10,000 /start, 100ms, default (512)", simpleWF, 0, 10000, 1000, 100 * time.Millisecond, start},
		{"10,000 /start 10k chats, 100ms, 2048", simpleWF, 2048, 10000, 10000, 100 * time.Millisecond, start},
		{"10,000 group msgs, 100ms, default", moderationWF, 0, 10000, 1000, 100 * time.Millisecond, groupMsg},
	}
	t.Logf("%-40s %10s %12s %14s %10s %10s", "case", "updates", "total", "updates/sec", "p50", "p99")
	for _, c := range cases {
		r := load(t, c.wf, c.workers, c.n, c.c, c.latency, c.mk)
		t.Logf("%-40s %10d %12s %14.0f %10s %10s", c.name, r.updates, r.elapsed.Round(time.Millisecond),
			float64(r.updates)/r.elapsed.Seconds(), r.p50.Round(10*time.Microsecond), r.p99.Round(10*time.Microsecond))
	}
}

// A group message that touches Redis (anti-flood counter + lock lookup)
// and Postgres (message log insert) before replying.
func BenchmarkServicesRedisPostgres(b *testing.B) {
	redisURL, dsn := os.Getenv("TGC_TEST_REDIS_URL"), os.Getenv("TGC_TEST_DB_DSN")
	if redisURL == "" || dsn == "" {
		b.Skip("set TGC_TEST_REDIS_URL and TGC_TEST_DB_DSN")
	}
	b.Setenv("TGC_REDIS_URL", redisURL)
	b.Setenv("TGC_DB_MAIN_DSN", dsn)
	wf := `{"name":"services","services":{"redis":{"url":"x"},"databases":{"main":{"driver":"postgres","dsn":"x",
	  "migrations":["CREATE TABLE IF NOT EXISTS msg_log (id BIGSERIAL PRIMARY KEY, chat_id BIGINT, user_id BIGINT, text TEXT)"]}}},
	  "nodes":[
	  {"id":"m","type":"trigger.message","params":{"chat_types":["supergroup"]}},
	  {"id":"flood","type":"redis.incr","params":{"key":"bench:flood:{{ chat.id }}:{{ from.id }}","ttl":10}},
	  {"id":"lock","type":"redis.get","params":{"key":"bench:lock:{{ chat.id }}"}},
	  {"id":"log","type":"db.exec","params":{"query":"INSERT INTO msg_log (chat_id, user_id, text) VALUES ($1, $2, $3)","args":["{{ chat.id }}","{{ from.id }}","{{ text }}"]}},
	  {"id":"reply","type":"telegram.send_message","params":{"text":"#{{ nodes.flood }}"}}],
	  "connections":{"m":{"main":["flood"]},"flood":{"main":["lock"]},"lock":{"main":["log"]},"log":{"main":["reply"]}}}`
	e := newEngine(b, wf, &memTG{}, nil)
	defer e.Close()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			i++
			e.HandleUpdate(ctx, msg(-100, "supergroup", float64(i%50), "سلام"))
		}
	})
}
