//go:build integration

package nodes

// Runs examples/anti-link against real Redis and Postgres:
//
//	TGC_REDIS_URL=redis://localhost:6379/0 \
//	TGC_DB_MAIN_DSN=postgres://... go test -tags integration ./internal/nodes/

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/mrjvadi/tgcreator/internal/engine"
	"github.com/mrjvadi/tgcreator/internal/tg"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

func TestAntiLinkExample(t *testing.T) {
	if os.Getenv("TGC_REDIS_URL") == "" || os.Getenv("TGC_DB_MAIN_DSN") == "" {
		t.Skip("TGC_REDIS_URL and TGC_DB_MAIN_DSN are required")
	}
	wf, err := workflow.Load("../../examples/anti-link/workflow.json")
	if err != nil {
		t.Fatal(err)
	}
	for i := range wf.Nodes {
		if wf.Nodes[i].ID == "wait" {
			wf.Nodes[i].Params["duration"] = "1ms"
		}
	}
	resetAdminCache()
	fake := &fakeTelegram{}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()
	e, err := engine.New(wf, engine.Options{Client: tg.New("T", srv.URL)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := e.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.SetBotInfo(map[string]any{"username": "my_bot"})

	rdb := e.Service("redis").(*redis.Client)
	db := e.Service("db:main").(*sql.DB)
	rdb.Del(ctx, "lock:link:-1001234567890")
	db.Exec("DELETE FROM locks")

	e.HandleUpdate(ctx, msgUpdate("supergroup", 2, "https://before-lock.com")) // not locked yet
	e.HandleUpdate(ctx, msgUpdate("supergroup", 2, "/lock link"))              // not admin
	e.HandleUpdate(ctx, msgUpdate("supergroup", 1, "/lock link"))              // admin
	e.HandleUpdate(ctx, msgUpdate("supergroup", 2, "join t.me/spam"))          // deleted + warned
	e.HandleUpdate(ctx, msgUpdate("supergroup", 1, "admin t.me/ok"))           // admin: allowed

	if v, _ := rdb.Get(ctx, "lock:link:-1001234567890").Result(); v != "1" {
		t.Errorf("redis lock = %q", v)
	}
	var setBy int64
	if err := db.QueryRow("SELECT set_by FROM locks WHERE chat_id = -1001234567890 AND kind = 'link'").Scan(&setBy); err != nil || setBy != 1 {
		t.Errorf("db row: set_by=%d err=%v", setBy, err)
	}
	got := strings.Join(fake.methods(), ",")
	want := "getChatMember,sendMessage,getChatMember,sendMessage,deleteMessage,sendMessage,deleteMessage"
	if got != want {
		t.Errorf("calls = %s\nwant    %s", got, want)
	}

	e.HandleUpdate(ctx, msgUpdate("supergroup", 1, "/unlock link"))
	if n, _ := rdb.Exists(ctx, "lock:link:-1001234567890").Result(); n != 0 {
		t.Error("lock should be removed from redis")
	}
}

func TestRedisStateBackend(t *testing.T) {
	if os.Getenv("TGC_REDIS_URL") == "" {
		t.Skip("TGC_REDIS_URL is required")
	}
	wf, err := workflow.Parse([]byte(`{"name":"t",
	  "runtime": {"state_backend": "redis", "state_ttl": "1m"},
	  "nodes": [
	    {"id":"a","type":"trigger.command","params":{"commands":["ask"]}},
	    {"id":"s","type":"state.set","params":{"values":{"step":"name"}}},
	    {"id":"b","type":"trigger.message","params":{"condition":"state.step == 'name' && command == ''"}},
	    {"id":"m","type":"telegram.send_message","params":{"text":"hi {{ text }}"}},
	    {"id":"c","type":"state.clear"}],
	  "connections":{"a":{"main":["s"]},"b":{"main":["m","c"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeTelegram{}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()
	e, err := engine.New(wf, engine.Options{Client: tg.New("T", srv.URL)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := e.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.HandleUpdate(ctx, msgUpdate("private", 9, "/ask"))
	if ttl, _ := e.Service("redis").(*redis.Client).TTL(ctx, "tgc:state:-1001234567890:9").Result(); ttl <= 0 {
		t.Errorf("state not stored in redis with ttl: %v", ttl)
	}
	e.HandleUpdate(ctx, msgUpdate("private", 9, "Reza"))
	e.HandleUpdate(ctx, msgUpdate("private", 9, "again")) // state cleared: ignored
	if got := strings.Join(fake.methods(), ","); got != "sendMessage" || fake.calls[0].Params["text"] != "hi Reza" {
		t.Errorf("calls = %s %#v", got, fake.calls)
	}
}
