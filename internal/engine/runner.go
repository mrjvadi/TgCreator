package engine

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mrjvadi/tgcreator/internal/tg"
	"github.com/mrjvadi/tgcreator/internal/tmpl"
)

// dispatcher shards updates by chat so updates of one chat are handled in
// order while different chats run in parallel.
type dispatcher struct {
	e      *Engine
	queues []chan map[string]any
	wg     sync.WaitGroup
	count  atomic.Int64
}

func newDispatcher(ctx context.Context, e *Engine) *dispatcher {
	workers := e.WF.Runtime.Workers
	if workers <= 0 {
		workers = DefaultWorkers()
	}
	size := e.WF.Runtime.QueueSize
	if size <= 0 {
		size = 256
	}
	d := &dispatcher{e: e, queues: make([]chan map[string]any, workers)}
	for i := range d.queues {
		q := make(chan map[string]any, size)
		d.queues[i] = q
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			for u := range q {
				d.handle(ctx, u)
			}
		}()
	}
	return d
}

func (d *dispatcher) handle(ctx context.Context, u map[string]any) {
	bg := &sync.WaitGroup{}
	defer func() {
		if r := recover(); r != nil {
			d.e.log.Error("panic while handling update", "panic", r)
		}
		d.count.Add(1)
		if d.e.onHandled != nil {
			go func() {
				bg.Wait()
				d.e.onHandled(u)
			}()
		}
	}()
	bg = d.e.handle(ctx, u)
}

func (d *dispatcher) push(u map[string]any) {
	typ := UpdateType(u)
	p := asMap(u[typ])
	key := asMap(p["chat"])["id"]
	if key == nil {
		key = asMap(asMap(p["message"])["chat"])["id"]
	}
	if key == nil {
		key = asMap(p["from"])["id"]
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(tmpl.ToString(key)))
	d.queues[h.Sum32()%uint32(len(d.queues))] <- u
}

func (d *dispatcher) close() {
	for _, q := range d.queues {
		close(q)
	}
	d.wg.Wait()
}

// Run connects to Telegram and processes updates until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) error {
	if e.tg == nil {
		if e.WF.Bot.Token == "" {
			return errors.New("bot token is empty (set bot.token or TGC_BOT_TOKEN)")
		}
		e.tg = tg.New(e.WF.Bot.Token, e.WF.Bot.APIURL)
	}
	var me map[string]any
	if err := e.tg.CallInto(ctx, "getMe", nil, &me); err != nil {
		return err
	}
	e.SetBotInfo(me)
	e.log.Info("bot connected", "username", e.botUsername, "updates", e.AllowedUpdates(), "services", e.requires)

	// Handlers keep running after ctx is cancelled so in-flight flows can
	// finish; the dispatcher drains before returning.
	d := newDispatcher(context.WithoutCancel(ctx), e)
	e.stop = ctx
	defer func() {
		d.close()
		e.deferred.Wait()
	}()

	if addr := e.WF.Runtime.HealthListen; addr != "" {
		go e.serveHealth(ctx, addr, d)
	}
	if e.WF.Bot.Mode == "webhook" {
		return e.runWebhook(ctx, d)
	}
	return e.runPolling(ctx, d)
}

func (e *Engine) runPolling(ctx context.Context, d *dispatcher) error {
	if err := e.callSilently(ctx, "deleteWebhook", map[string]any{"drop_pending_updates": e.WF.Runtime.DropPending}); err != nil {
		return err
	}
	allowed := e.AllowedUpdates()
	var offset int64
	backoff := time.Second
	for {
		ups, err := e.tg.GetUpdates(ctx, offset, 50, allowed)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			e.log.Error("getUpdates failed", "err", err, "retry_in", backoff)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		backoff = time.Second
		for _, u := range ups {
			if id, ok := tmpl.ToInt64(u["update_id"]); ok && id >= offset {
				offset = id + 1
			}
			d.push(u)
		}
	}
}

func (e *Engine) runWebhook(ctx context.Context, d *dispatcher) error {
	wh := e.WF.Bot.Webhook
	listen, path := wh.Listen, wh.Path
	if listen == "" {
		listen = ":8080"
	}
	if path == "" {
		path = "/webhook"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
		if wh.SecretToken != "" && r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != wh.SecretToken {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var u map[string]any
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&u); err != nil {
			http.Error(w, "bad update", http.StatusBadRequest)
			return
		}
		d.push(u)
		w.WriteHeader(http.StatusOK)
	})
	srv := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	if wh.URL != "" {
		params := map[string]any{
			"url":                  wh.URL,
			"allowed_updates":      e.AllowedUpdates(),
			"drop_pending_updates": e.WF.Runtime.DropPending,
		}
		if wh.SecretToken != "" {
			params["secret_token"] = wh.SecretToken
		}
		if wh.MaxConnections > 0 {
			params["max_connections"] = wh.MaxConnections
		}
		if err := e.callSilently(ctx, "setWebhook", params); err != nil {
			return err
		}
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	e.log.Info("webhook listening", "addr", listen, "path", path)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	}
}

func (e *Engine) callSilently(ctx context.Context, method string, params map[string]any) error {
	_, err := e.tg.Call(ctx, method, params)
	return err
}

func (e *Engine) serveHealth(ctx context.Context, addr string, d *dispatcher) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"bot":      e.botUsername,
			"handled":  d.count.Load(),
			"services": e.requires,
		})
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		e.log.Error("health server", "err", err)
	}
}
