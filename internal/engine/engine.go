// Package engine compiles a workflow into an executable graph and runs it
// against Telegram updates.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mrjvadi/tgcreator/internal/tg"
	"github.com/mrjvadi/tgcreator/internal/tmpl"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

type node struct {
	spec    workflow.Node
	action  Action
	trigger Trigger
	next    map[string][]*node
}

type Engine struct {
	WF  *workflow.Workflow
	log *slog.Logger
	tg  *tg.Client

	nodes    map[string]*node
	triggers map[string][]*node // update type -> triggers; "*" = any
	requires []string
	useState bool

	services map[string]any
	closers  []io.Closer
	state    StateStore
	stateTTL time.Duration

	globals     map[string]any
	publicEnv   map[string]any
	botInfo     map[string]any
	botUsername string
	maxSteps    int
	timeout     time.Duration
	onHandled   func(u map[string]any)

	stop     context.Context // cancelled on shutdown; aborts pending delays
	deferred sync.WaitGroup  // all delayed steps, waited for on shutdown
}

// Options configure New.
type Options struct {
	Logger *slog.Logger
	// Client overrides the Telegram client (tests, custom transports).
	Client *tg.Client
	// OnUpdateHandled is called after every update has been fully handled
	// by Run (metrics, tests).
	OnUpdateHandled func(u map[string]any)
}

// New compiles a workflow. It does not connect to anything, so it is also
// used by `validate` and `compose`.
func New(wf *workflow.Workflow, opts Options) (*Engine, error) {
	e := &Engine{
		WF:        wf,
		log:       opts.Logger,
		tg:        opts.Client,
		nodes:     make(map[string]*node, len(wf.Nodes)),
		triggers:  map[string][]*node{},
		services:  map[string]any{},
		globals:   wf.Variables,
		publicEnv: publicEnv(),
		botInfo:   map[string]any{},
		maxSteps:  wf.Runtime.MaxSteps,
		onHandled: opts.OnUpdateHandled,
		stop:      context.Background(),
	}
	if e.log == nil {
		e.log = slog.Default()
	}
	if e.maxSteps <= 0 {
		e.maxSteps = 1000
	}
	e.timeout = 60 * time.Second
	if wf.Runtime.HandleTimeout != "" {
		d, err := time.ParseDuration(wf.Runtime.HandleTimeout)
		if err != nil {
			return nil, fmt.Errorf("runtime.handle_timeout: %w", err)
		}
		e.timeout = d
	}
	if wf.Runtime.StateTTL != "" {
		d, err := time.ParseDuration(wf.Runtime.StateTTL)
		if err != nil {
			return nil, fmt.Errorf("runtime.state_ttl: %w", err)
		}
		e.stateTTL = d
	}
	if err := e.compile(); err != nil {
		return nil, err
	}
	return e, nil
}

func publicEnv() map[string]any {
	out := map[string]any{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(k, "TGC_VAR_") {
			out[strings.TrimPrefix(k, "TGC_VAR_")] = v
		}
	}
	return out
}

func (e *Engine) compile() error {
	b := &Build{Workflow: e.WF, requires: map[string]bool{}}
	for _, spec := range e.WF.Nodes {
		if spec.Disabled {
			continue
		}
		nt := lookup(spec.Type)
		if nt == nil {
			return fmt.Errorf("node %s: unknown type %q (unknown node, or its module was excluded from this build)", spec.ID, spec.Type)
		}
		if spec.Params == nil {
			spec.Params = map[string]any{}
		}
		impl, err := nt.New(b, spec)
		if err != nil {
			return fmt.Errorf("node %s (%s): %w", spec.ID, spec.Type, err)
		}
		n := &node{spec: spec, next: map[string][]*node{}}
		switch t := impl.(type) {
		case Trigger:
			n.trigger = t
			for _, ut := range t.UpdateTypes() {
				e.triggers[ut] = append(e.triggers[ut], n)
			}
		case Action:
			n.action = t
		default:
			return fmt.Errorf("node %s: type %s built neither action nor trigger", spec.ID, spec.Type)
		}
		if nt.Requires != nil {
			for _, r := range nt.Requires(spec.Params) {
				b.requires[r] = true
			}
		}
		e.nodes[spec.ID] = n
	}
	if len(e.triggers) == 0 {
		return errors.New("workflow has no trigger node")
	}
	for from, outs := range e.WF.Connections {
		src, ok := e.nodes[from]
		if !ok {
			continue // disabled
		}
		for out, targets := range outs {
			for _, to := range targets {
				if dst, ok := e.nodes[to]; ok {
					if dst.trigger != nil {
						return fmt.Errorf("connection %s -> %s: cannot connect into a trigger", from, to)
					}
					src.next[out] = append(src.next[out], dst)
				}
			}
		}
	}

	e.useState = b.state
	if e.useState {
		backend := e.WF.Runtime.StateBackend
		if backend == "" {
			backend = "memory"
		}
		sb, ok := stateBackends[backend]
		if !ok {
			return fmt.Errorf("runtime.state_backend: unknown backend %q", backend)
		}
		for _, r := range sb.requires {
			b.requires[r] = true
		}
	}
	for r := range b.requires {
		e.requires = append(e.requires, r)
	}
	sort.Strings(e.requires)
	return nil
}

// Requirements lists the services this workflow needs ("redis", "db:main").
func (e *Engine) Requirements() []string { return e.requires }

// AllowedUpdates is the minimal allowed_updates list for Telegram.
func (e *Engine) AllowedUpdates() []string {
	if _, all := e.triggers["*"]; all {
		return tg.AllUpdateTypes
	}
	out := make([]string, 0, len(e.triggers))
	for t := range e.triggers {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Service returns an opened service, e.g. Service("redis").
func (e *Engine) Service(req string) any { return e.services[req] }

// Logger returns the engine logger.
func (e *Engine) Logger() *slog.Logger { return e.log }

// Open connects only the services the workflow requires, then initializes
// nodes and the state store.
func (e *Engine) Open(ctx context.Context) error {
	for _, req := range e.requires {
		kind, name, _ := strings.Cut(req, ":")
		p := serviceProvider(kind)
		if p == nil {
			return fmt.Errorf("service %q is required but this binary was built without the %s module", req, kind)
		}
		svc, closer, err := p(ctx, e, name)
		if err != nil {
			return fmt.Errorf("open %s: %w", req, err)
		}
		e.services[req] = svc
		if closer != nil {
			e.closers = append(e.closers, closer)
		}
		e.log.Info("service ready", "service", req)
	}
	if e.useState {
		backend := e.WF.Runtime.StateBackend
		if backend == "" {
			backend = "memory"
		}
		st, err := stateBackends[backend].f(e)
		if err != nil {
			return fmt.Errorf("state backend %s: %w", backend, err)
		}
		e.state = st
	}
	for _, n := range e.nodes {
		var impl any = n.action
		if n.trigger != nil {
			impl = n.trigger
		}
		if in, ok := impl.(Initializer); ok {
			if err := in.Init(e); err != nil {
				return fmt.Errorf("init node %s: %w", n.spec.ID, err)
			}
		}
	}
	return nil
}

// StateTTL is the configured lifetime of user state (0 = forever).
func (e *Engine) StateTTL() time.Duration { return e.stateTTL }

// Close releases services.
func (e *Engine) Close() {
	for i := len(e.closers) - 1; i >= 0; i-- {
		_ = e.closers[i].Close()
	}
}

// SetBotInfo sets the getMe result (used for /cmd@bot matching).
func (e *Engine) SetBotInfo(me map[string]any) {
	e.botInfo = me
	e.botUsername = tmpl.ToString(me["username"])
}

// HandleUpdate runs every trigger matching the update. Safe for concurrent use.
// It returns once the update is completely handled, including steps
// scheduled after a logic.delay.
func (e *Engine) HandleUpdate(ctx context.Context, u map[string]any) {
	e.handle(ctx, u).Wait()
}

// handle runs the synchronous part of the flows; delayed steps continue in
// the background and are tracked by the returned WaitGroup.
func (e *Engine) handle(ctx context.Context, u map[string]any) *sync.WaitGroup {
	bg := &sync.WaitGroup{}
	typ := UpdateType(u)
	cands := e.triggers[typ]
	if wild := e.triggers["*"]; len(wild) > 0 {
		cands = append(cands[:len(cands):len(cands)], wild...)
	}
	if len(cands) == 0 {
		return bg
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	base := e.baseEnv(u, typ)
	var stateKey string
	if e.useState {
		stateKey = stateKeyFor(base)
		st := map[string]any{}
		if stateKey != "" {
			if s, err := e.state.Get(ctx, stateKey); err != nil {
				e.log.Error("load state", "err", err)
			} else if s != nil {
				st = s
			}
		}
		base["state"] = st
	}

	for _, t := range cands {
		x := e.newExec(ctx, u, typ, base)
		x.stateKey = stateKey
		x.bg = bg
		data, ok := t.trigger.Match(x)
		if !ok {
			continue
		}
		x.Results[t.spec.ID] = data
		e.run(x, t.next[Main])
		// State changes made by one flow are visible to the next trigger.
		if st, ok := x.Env["state"]; ok {
			base["state"] = st
		}
	}
	return bg
}

func stateKeyFor(env tmpl.Env) string {
	chat := tmpl.ToString(asMap(env["chat"])["id"])
	user := tmpl.ToString(asMap(env["from"])["id"])
	if chat == "" && user == "" {
		return ""
	}
	return chat + ":" + user
}

func (e *Engine) run(x *Exec, start []*node) {
	stack := make([]*node, 0, 16)
	for i := len(start) - 1; i >= 0; i-- {
		stack = append(stack, start[i])
	}
	for len(stack) > 0 {
		x.steps++
		if x.steps > e.maxSteps {
			e.log.Error("max steps reached, stopping flow", "limit", e.maxSteps)
			return
		}
		if x.Ctx.Err() != nil {
			e.log.Error("flow aborted", "err", x.Ctx.Err())
			return
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		x.cur = n
		res, err := n.action.Exec(x)
		if err != nil {
			errData := map[string]any{"error": err.Error(), "node": n.spec.ID}
			x.Results[n.spec.ID] = errData
			x.Env["error"] = errData
			switch {
			case len(n.next["error"]) > 0:
				res = Result{Output: "error"}
			case n.spec.ContinueOnError:
				e.log.Warn("node failed, continuing", "node", n.spec.ID, "err", err)
				res = Result{Output: Main}
			default:
				e.log.Error("node failed", "node", n.spec.ID, "type", n.spec.Type, "err", err)
				continue
			}
		} else {
			x.Results[n.spec.ID] = res.Data
		}
		targets := n.next[res.Output]
		for i := len(targets) - 1; i >= 0; i-- {
			stack = append(stack, targets[i])
		}
	}
}

// DefaultWorkers is used when runtime.workers is not set.
func DefaultWorkers() int { return runtime.NumCPU() * 4 }
