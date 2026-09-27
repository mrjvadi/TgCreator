package panel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/mrjvadi/tgcreator/internal/engine"
	"github.com/mrjvadi/tgcreator/internal/tg"
	"github.com/mrjvadi/tgcreator/internal/tgsim"
	"github.com/mrjvadi/tgcreator/internal/workflow"
	"github.com/mrjvadi/tgcreator/internal/xui/xuifake"
)

// Test world: who and where the panel user can chat as.
const (
	UserMe     = 1001
	UserOther  = 1002
	ChatGroup  = -100777
	ChatChanel = -100888
)

// session runs a workflow on the real runtime against the simulator.
type session struct {
	id      string
	channel string
	sim     *tgsim.Sim
	eng     *engine.Engine
	mini    *miniredis.Miniredis
	panels  []*xuifake.Panel
	cancel  context.CancelFunc
	done    chan struct{}
	last    atomic.Int64
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// startSession prepares a sandbox: a fake Telegram with a private chat, a
// supergroup and a channel, an in-memory Redis, and the workflow running
// on the real engine.
func (s *Server) startSession(wf *workflow.Workflow) (*session, error) {
	wf.Bot.Mode = "polling"
	wf.Bot.Token = "sim"
	wf.Runtime.HealthListen = ""
	wf.Runtime.DropPending = false

	probe, err := engine.New(wf, engine.Options{})
	if err != nil {
		return nil, err
	}
	sess := &session{id: newID(), done: make(chan struct{})}
	sess.channel = "test:" + sess.id
	sess.last.Store(time.Now().Unix())

	for _, r := range probe.Requirements() {
		kind, name, _ := strings.Cut(r, ":")
		switch kind {
		case "redis":
			// Tests never touch real Redis data.
			sess.mini = miniredis.NewMiniRedis()
			if err := sess.mini.Start(); err != nil {
				return nil, err
			}
			wf.Services.Redis = &workflow.Redis{URL: "redis://" + sess.mini.Addr()}
		case "db":
			db := wf.Services.Databases[name]
			if dsn := os.Getenv("TGC_TEST_" + strings.TrimPrefix(workflow.DSNEnv(name), "TGC_")); dsn != "" {
				db.DSN = dsn
			}
			db.DSN = workflow.ExpandEnv(db.DSN)
			wf.Services.Databases[name] = db
		case "xui":
			// Tests never create users on a real panel: an in-memory panel
			// stands in unless TGC_TEST_XUI_<NAME>_URL names a test panel.
			p := wf.Services.XUI[name]
			env := "TGC_TEST_" + strings.TrimPrefix(workflow.XUIEnv(name), "TGC_")
			if u := os.Getenv(env + "_URL"); u != "" {
				p.URL, p.Username, p.Password = u, os.Getenv(env+"_USERNAME"), os.Getenv(env+"_PASSWORD")
				p.TOTPSecret = os.Getenv(env + "_TOTP")
			} else {
				fake := xuifake.New(p.Type)
				sess.panels = append(sess.panels, fake)
				p.Address = workflow.ExpandEnv(p.Address)
				if p.Address == "" {
					p.Address = "vpn.example.com"
					if u, err := url.Parse(workflow.ExpandEnv(p.URL)); err == nil && u.Hostname() != "" {
						p.Address = u.Hostname()
					}
				}
				p.Type, p.URL, p.Username, p.Password = fake.Type, fake.URL, fake.Username, fake.Password
				p.TOTPSecret, p.APIPath, p.SubURL, p.Insecure = "", "", "", false
			}
			if wf.Services.XUI == nil {
				wf.Services.XUI = map[string]workflow.XUIPanel{}
			}
			wf.Services.XUI[name] = p
		}
	}
	if wf.Runtime.StateBackend == "redis" && sess.mini == nil {
		wf.Runtime.StateBackend = "memory"
	}

	sim := tgsim.New("tgcreator_test_bot")
	sess.sim = sim
	sim.AddUser(UserMe, "شما", "me")
	sim.AddUser(UserOther, "کاربر تست", "tester")
	sim.AddChat(ChatGroup, "supergroup", "گروه تست", "test_group")
	sim.SetMember(ChatGroup, UserMe, "creator")
	sim.SetMember(ChatGroup, UserOther, "member")
	sim.SetMember(ChatGroup, sim.BotID(), "administrator", tgsim.AdminRights...)
	sim.AddChat(ChatChanel, "channel", "کانال تست", "test_channel")
	sim.SetMember(ChatChanel, UserMe, "creator")
	sim.SetMember(ChatChanel, sim.BotID(), "administrator", tgsim.AdminRights...)

	if len(sess.panels) > 0 {
		sim.Log(0, "پنل X-UI در این تست شبیه‌سازی شده است (اینباند ۱: VLESS Reality، اینباند ۲: VMess WS)؛ هیچ کاربری روی پنل واقعی ساخته نمی‌شود.")
	}

	logger := slog.New(&simLog{sim: sim})
	eng, err := engine.New(wf, engine.Options{Client: tg.New(sim.Token, sim.URL), Logger: logger, OnUpdateHandled: sim.Handled})
	if err != nil {
		sess.close()
		return nil, err
	}
	sess.eng = eng
	ctx, cancel := context.WithCancel(context.Background())
	sess.cancel = cancel
	octx, ocancel := context.WithTimeout(ctx, 6*time.Second)
	defer ocancel()
	if err := eng.Open(octx); err != nil {
		sess.close()
		msg := "اتصال به سرویس‌ها ناموفق بود: " + err.Error()
		for _, r := range probe.Requirements() {
			if name, ok := strings.CutPrefix(r, "db:"); ok && strings.Contains(err.Error(), r) {
				msg = fmt.Sprintf("این workflow به دیتابیس «%s» نیاز دارد ولی پنل به آن وصل نشد (%v). "+
					"برای تست، DSN یک دیتابیس آزمایشی را در متغیر محیطی %s پنل بدهید؛ Redis لازم نیست و در حافظه ساخته می‌شود.",
					name, err, "TGC_TEST_"+strings.TrimPrefix(workflow.DSNEnv(name), "TGC_"))
			}
		}
		return nil, errors.New(msg)
	}

	sim.OnEvent(func(ev tgsim.Event) {
		msg := map[string]any{"type": "event", "event": ev}
		if ev.ChatID != 0 {
			snap := sim.Snapshot(ev.ChatID)
			msg["chat"] = snap
		}
		pctx, pcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer pcancel()
		if err := s.rt.Publish(pctx, sess.channel, msg); err != nil {
			s.log.Warn("publish", "err", err)
		}
	})
	go func() {
		defer close(sess.done)
		if err := eng.Run(ctx); err != nil {
			sim.Log(0, "ران‌تایم متوقف شد: "+err.Error())
		}
	}()
	return sess, nil
}

func (sess *session) close() {
	if sess.cancel != nil {
		sess.cancel()
		select {
		case <-sess.done:
		case <-time.After(5 * time.Second):
		}
	}
	if sess.eng != nil {
		sess.eng.Close()
	}
	if sess.sim != nil {
		sess.sim.Close()
	}
	if sess.mini != nil {
		sess.mini.Close()
	}
	for _, p := range sess.panels {
		p.Close()
	}
}

// simLog shows runtime warnings and errors (failed nodes...) in the test chat.
type simLog struct {
	sim   *tgsim.Sim
	attrs []slog.Attr
}

func (h *simLog) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelWarn }

func (h *simLog) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)
	write := func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value)
		return true
	}
	for _, a := range h.attrs {
		write(a)
	}
	r.Attrs(write)
	h.sim.Log(0, b.String())
	return nil
}

func (h *simLog) WithAttrs(as []slog.Attr) slog.Handler {
	return &simLog{sim: h.sim, attrs: append(append([]slog.Attr{}, h.attrs...), as...)}
}

func (h *simLog) WithGroup(string) slog.Handler { return h }

type sessions struct {
	mu sync.Mutex
	m  map[string]*session
}

func (ss *sessions) get(id string) *session {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	s := ss.m[id]
	if s != nil {
		s.last.Store(time.Now().Unix())
	}
	return s
}
