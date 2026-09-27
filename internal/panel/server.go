// Package panel is the HTTP server behind the visual workflow builder:
// node catalog, validation, compose export and live test sessions whose
// events are pushed to the browser through Centrifugo.
package panel

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mrjvadi/tgcreator/examples"
	"github.com/mrjvadi/tgcreator/internal/compose"
	"github.com/mrjvadi/tgcreator/internal/engine"
	"github.com/mrjvadi/tgcreator/internal/realtime"
	"github.com/mrjvadi/tgcreator/internal/tg"
	"github.com/mrjvadi/tgcreator/internal/tgsim"
	"github.com/mrjvadi/tgcreator/internal/workflow"
	"github.com/mrjvadi/tgcreator/web"
)

type Config struct {
	Password    string // optional HTTP basic auth for the whole panel
	MaxSessions int
	SessionIdle time.Duration
	Realtime    realtime.Config
}

type Server struct {
	cfg      Config
	log      *slog.Logger
	rt       *realtime.Centrifugo
	spec     *tgsim.Spec
	sessions sessions
	mux      *http.ServeMux
	stop     chan struct{}
	stopOnce sync.Once
}

func New(cfg Config, log *slog.Logger) *Server {
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 20
	}
	if cfg.SessionIdle <= 0 {
		cfg.SessionIdle = 20 * time.Minute
	}
	if log == nil {
		log = slog.Default()
	}
	s := &Server{cfg: cfg, log: log, rt: realtime.New(cfg.Realtime), spec: tgsim.LoadSpec(),
		sessions: sessions{m: map[string]*session{}}, mux: http.NewServeMux(), stop: make(chan struct{})}
	s.routes()
	go s.janitor()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Password != "" {
		_, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(pass), []byte(s.cfg.Password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="TgCreator"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

// Close stops every test session.
func (s *Server) Close() {
	s.stopOnce.Do(func() { close(s.stop) })
	s.sessions.mu.Lock()
	all := s.sessions.m
	s.sessions.m = map[string]*session{}
	s.sessions.mu.Unlock()
	for _, sess := range all {
		sess.close()
	}
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/catalog", s.catalog)
	s.mux.HandleFunc("GET /api/botapi/methods", s.botMethods)
	s.mux.HandleFunc("GET /api/examples", s.listExamples)
	s.mux.HandleFunc("GET /api/examples/{id}", s.getExample)
	s.mux.HandleFunc("POST /api/validate", s.validate)
	s.mux.HandleFunc("POST /api/compose", s.compose)
	s.mux.HandleFunc("GET /api/realtime", s.realtimeInfo)
	s.mux.HandleFunc("POST /api/test", s.testStart)
	s.mux.HandleFunc("POST /api/test/{id}/action", s.testAction)
	s.mux.HandleFunc("GET /api/test/{id}/chats/{chat}", s.testChat)
	s.mux.HandleFunc("GET /api/test/{id}/events", s.testEvents)
	s.mux.HandleFunc("DELETE /api/test/{id}", s.testStop)

	dist, _ := fs.Sub(web.Dist, "dist")
	files := http.FileServerFS(dist)
	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// Single-page app: unknown paths serve index.html.
		if _, err := fs.Stat(dist, strings.TrimPrefix(path.Clean(r.URL.Path), "/")); err != nil || r.URL.Path == "/" {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func readWorkflow(r *http.Request) (*workflow.Workflow, error) {
	var wf workflow.Workflow
	dec := json.NewDecoder(io.LimitReader(r.Body, 5<<20))
	if err := dec.Decode(&wf); err != nil {
		return nil, errors.New("فایل workflow نامعتبر است: " + err.Error())
	}
	return &wf, nil
}

func (s *Server) catalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{
		"nodes":        engine.NodeTypes(),
		"bot_api":      s.spec.Version,
		"update_types": tg.AllUpdateTypes,
	})
}

func (s *Server) botMethods(w http.ResponseWriter, _ *http.Request) {
	type field struct {
		Name     string   `json:"name"`
		Types    []string `json:"types"`
		Required bool     `json:"required"`
	}
	type method struct {
		Name   string  `json:"name"`
		Fields []field `json:"fields"`
	}
	var out []method
	for _, name := range s.spec.MethodNames() {
		m := method{Name: name, Fields: []field{}}
		for _, f := range s.spec.Methods[name].Fields {
			m.Fields = append(m.Fields, field{f.Name, f.Types, f.Required})
		}
		out = append(out, m)
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, 200, map[string]any{"version": s.spec.Version, "methods": out})
}

func (s *Server) listExamples(w http.ResponseWriter, _ *http.Request) {
	type item struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Nodes int    `json:"nodes"`
	}
	var out []item
	entries, _ := fs.ReadDir(examples.FS, ".")
	for _, e := range entries {
		raw, err := fs.ReadFile(examples.FS, e.Name()+"/workflow.json")
		if err != nil {
			continue
		}
		var wf workflow.Workflow
		if json.Unmarshal(raw, &wf) == nil {
			out = append(out, item{ID: e.Name(), Name: wf.Name, Nodes: len(wf.Nodes)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Nodes < out[j].Nodes })
	writeJSON(w, 200, out)
}

func (s *Server) getExample(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.ContainsAny(id, "/\\.") {
		fail(w, 400, "bad id")
		return
	}
	raw, err := fs.ReadFile(examples.FS, id+"/workflow.json")
	if err != nil {
		fail(w, 404, "not found")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(raw)
}

func (s *Server) validate(w http.ResponseWriter, r *http.Request) {
	wf, err := readWorkflow(r)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	issues := engine.Check(wf)
	resp := map[string]any{"issues": issues, "ok": true}
	for _, is := range issues {
		if is.Level == "error" {
			resp["ok"] = false
		}
	}
	if resp["ok"] == true {
		if e, err := engine.New(wf, engine.Options{}); err == nil {
			resp["requirements"] = e.Requirements()
			resp["updates"] = e.AllowedUpdates()
		} else {
			resp["ok"] = false
			resp["issues"] = append(issues, engine.Issue{Level: "error", Message: err.Error()})
		}
	}
	writeJSON(w, 200, resp)
}

func (s *Server) compose(w http.ResponseWriter, r *http.Request) {
	wf, err := readWorkflow(r)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	e, err := engine.New(wf, engine.Options{})
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	opt := compose.Options{WorkflowFile: "./workflow.json", Image: r.URL.Query().Get("image")}
	if r.URL.Query().Get("assets") == "1" {
		opt.ExtraMounts = []string{"./assets:/app/assets:ro"}
	}
	yml, err := compose.Generate(wf, e.Requirements(), opt)
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="docker-compose.yml"`)
	_, _ = io.WriteString(w, yml)
}

func (s *Server) realtimeInfo(w http.ResponseWriter, _ *http.Request) {
	if !s.rt.Enabled() {
		writeJSON(w, 200, map[string]any{"enabled": false})
		return
	}
	// Anonymous connection; private test channels still need their own
	// subscription token issued with the session.
	tok, err := s.rt.ConnectionToken("", time.Hour)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"enabled": true, "ws_url": s.rt.WSURL(), "token": tok})
}

func (s *Server) testStart(w http.ResponseWriter, r *http.Request) {
	wf, err := readWorkflow(r)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	for _, is := range engine.Check(wf) {
		if is.Level == "error" {
			writeJSON(w, 422, map[string]any{"error": "workflow خطا دارد؛ اول خطاها را برطرف کنید", "issues": engine.Check(wf)})
			return
		}
	}
	s.sessions.mu.Lock()
	n := len(s.sessions.m)
	s.sessions.mu.Unlock()
	if n >= s.cfg.MaxSessions {
		fail(w, 429, "تعداد تست‌های هم‌زمان به سقف رسیده است")
		return
	}
	sess, err := s.startSession(wf)
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	s.sessions.mu.Lock()
	s.sessions.m[sess.id] = sess
	s.sessions.mu.Unlock()

	resp := map[string]any{
		"id":      sess.id,
		"channel": sess.channel,
		"users":   []map[string]any{{"id": UserMe, "name": "شما (مالک گروه)"}, {"id": UserOther, "name": "کاربر تست (عضو عادی)"}},
		"chats":   sess.sim.ChatList(),
	}
	if s.rt.Enabled() {
		if tok, err := s.rt.SubscriptionToken("", sess.channel, 2*time.Hour); err == nil {
			resp["sub_token"] = tok
		}
	}
	writeJSON(w, 200, resp)
}

type action struct {
	Type      string `json:"type"` // send, press, join, contact, post, inline, reply
	ChatID    int64  `json:"chat_id"`
	UserID    int64  `json:"user_id"`
	Text      string `json:"text"`
	MessageID int64  `json:"message_id"`
	Button    string `json:"button"`
}

func (s *Server) testAction(w http.ResponseWriter, r *http.Request) {
	sess := s.sessions.get(r.PathValue("id"))
	if sess == nil {
		fail(w, 404, "جلسهٔ تست پیدا نشد (منقضی شده؟)")
		return
	}
	var a action
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&a); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if a.UserID == 0 {
		a.UserID = UserMe
	}
	if !sess.sim.HasUser(a.UserID) {
		fail(w, 400, "کاربر نامعتبر")
		return
	}
	sim := sess.sim
	var err error
	switch a.Type {
	case "send":
		if a.ChatID == ChatChanel {
			sim.ChannelPost(a.ChatID, a.Text)
		} else if strings.TrimSpace(a.Text) != "" {
			if a.MessageID != 0 {
				sim.ReplyByID(a.ChatID, a.UserID, a.MessageID, a.Text)
			} else {
				sim.Send(a.ChatID, a.UserID, a.Text)
			}
		}
	case "press":
		err = sim.PressByID(a.UserID, a.ChatID, a.MessageID, a.Button)
	case "join":
		sim.Join(a.ChatID, a.UserID)
	case "contact":
		sim.SendWith(a.ChatID, a.UserID, "", map[string]any{"contact": map[string]any{
			"phone_number": "+989120000000", "first_name": sim.UserName(a.UserID), "user_id": float64(a.UserID)}})
	case "inline":
		sim.InlineQuery(a.UserID, a.Text)
	default:
		err = errors.New("unknown action " + a.Type)
	}
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	// Give the runtime a moment so the response already includes the reply
	// (live updates still arrive over Centrifugo).
	_ = sim.WaitIdle(2 * time.Second)
	writeJSON(w, 200, map[string]any{"ok": true, "chat": sim.Snapshot(a.ChatID)})
}

func (s *Server) testChat(w http.ResponseWriter, r *http.Request) {
	sess := s.sessions.get(r.PathValue("id"))
	if sess == nil {
		fail(w, 404, "جلسهٔ تست پیدا نشد")
		return
	}
	chat, _ := strconv.ParseInt(r.PathValue("chat"), 10, 64)
	writeJSON(w, 200, sess.sim.Snapshot(chat))
}

// testEvents is the polling fallback when Centrifugo is not configured.
func (s *Server) testEvents(w http.ResponseWriter, r *http.Request) {
	sess := s.sessions.get(r.PathValue("id"))
	if sess == nil {
		fail(w, 404, "جلسهٔ تست پیدا نشد")
		return
	}
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	writeJSON(w, 200, map[string]any{"events": sess.sim.EventsSince(after)})
}

func (s *Server) testStop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.sessions.mu.Lock()
	sess := s.sessions.m[id]
	delete(s.sessions.m, id)
	s.sessions.mu.Unlock()
	if sess != nil {
		go sess.close()
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) janitor() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
		}
		cutoff := time.Now().Add(-s.cfg.SessionIdle).Unix()
		s.sessions.mu.Lock()
		for id, sess := range s.sessions.m {
			if sess.last.Load() < cutoff {
				delete(s.sessions.m, id)
				go sess.close()
			}
		}
		s.sessions.mu.Unlock()
	}
}

// ListenAndServe runs the panel until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{Addr: addr, Handler: s, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	s.log.Info("panel listening", "addr", addr, "realtime", s.rt.Enabled())
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		s.Close()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	}
}
