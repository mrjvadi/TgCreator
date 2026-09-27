package engine

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/mrjvadi/tgcreator/internal/tmpl"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// Result is what an action node returns: which output to follow and the
// data stored under nodes.<id> for later expressions.
type Result struct {
	Output string // "" stops this branch
	Data   any
}

// Main is the default output name.
const Main = "main"

// Action is a node executed as part of a flow.
type Action interface {
	Exec(x *Exec) (Result, error)
}

// Trigger starts flows. UpdateTypes lets the engine index triggers and ask
// Telegram only for the update types the workflow actually uses.
type Trigger interface {
	UpdateTypes() []string // "*" = every type
	Match(x *Exec) (data any, ok bool)
}

// Initializer is implemented by nodes that need services (redis, db...)
// once they are connected.
type Initializer interface {
	Init(e *Engine) error
}

// Param describes one parameter of a node for the visual builder.
type Param struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	// Type drives the editor: text, textarea, expr, number, bool, select,
	// tags, buttons, keyboard, json, vars, sql, duration, file, method.
	Type        string   `json:"type"`
	Required    bool     `json:"required,omitempty"`
	Default     any      `json:"default,omitempty"`
	Options     []string `json:"options,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
}

// Meta is what the visual builder needs to draw and edit a node.
type Meta struct {
	Label    string   `json:"label"`
	Summary  string   `json:"summary"`        // one line shown in the node picker
	Category string   `json:"category"`       // trigger, telegram, logic, state, redis, db, http
	Icon     string   `json:"icon,omitempty"` // lucide icon name
	Outputs  []string `json:"outputs"`        // named outputs; empty for none
	// CaseOutputs names a tags param whose values become extra outputs
	// (logic.switch cases).
	CaseOutputs string  `json:"case_outputs,omitempty"`
	Params      []Param `json:"params"`
	// Method is the Bot API method whose remaining fields may be passed
	// through as extra parameters.
	Method string `json:"method,omitempty"`
}

// NodeType describes a node kind that the web builder can place.
type NodeType struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Meta        Meta   `json:"meta"`
	// Requires returns the services the node needs, e.g. "redis" or "db:main".
	// Only required services are started.
	Requires func(params map[string]any) []string         `json:"-"`
	New      func(b *Build, n workflow.Node) (any, error) `json:"-"` // returns Action or Trigger
}

var (
	regMu    sync.RWMutex
	registry = map[string]*NodeType{}
	prefixes = map[string]*NodeType{} // e.g. "tg." handles tg.<anyMethod>
)

// Register adds a node type. A name ending in "." registers a prefix.
func Register(t NodeType) {
	regMu.Lock()
	defer regMu.Unlock()
	if strings.HasSuffix(t.Name, ".") {
		prefixes[t.Name] = &t
		return
	}
	if _, dup := registry[t.Name]; dup {
		panic("engine: duplicate node type " + t.Name)
	}
	registry[t.Name] = &t
}

var metas = map[string]Meta{}

// Describe attaches builder metadata to a node type (registered before or
// after; init order across files does not matter).
func Describe(name string, m Meta) {
	regMu.Lock()
	defer regMu.Unlock()
	if m.Outputs == nil {
		m.Outputs = []string{Main}
	}
	metas[name] = m
}

func lookup(name string) *NodeType {
	regMu.RLock()
	defer regMu.RUnlock()
	if t, ok := registry[name]; ok {
		return t
	}
	for p, t := range prefixes {
		if strings.HasPrefix(name, p) && len(name) > len(p) {
			return t
		}
	}
	return nil
}

// NodeTypes lists registered types (for `tgcreator nodes`).
func NodeTypes() []NodeType {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]NodeType, 0, len(registry)+len(prefixes))
	for _, t := range registry {
		out = append(out, *t)
	}
	for _, t := range prefixes {
		out = append(out, *t)
	}
	for i := range out {
		if m, ok := metas[out[i].Name]; ok {
			out[i].Meta = m
		} else {
			out[i].Meta = Meta{Label: out[i].Name, Category: "other", Outputs: []string{Main}}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ServiceProvider opens a service. kind is the part before ":" of a
// requirement ("redis", "db"), name the part after ("main").
type ServiceProvider func(ctx context.Context, e *Engine, name string) (svc any, closer io.Closer, err error)

var services = map[string]ServiceProvider{}

// RegisterService makes a service kind available. Modules left out of the
// build (build tags) simply never register.
func RegisterService(kind string, p ServiceProvider) {
	regMu.Lock()
	defer regMu.Unlock()
	services[kind] = p
}

func serviceProvider(kind string) ServiceProvider {
	regMu.RLock()
	defer regMu.RUnlock()
	return services[kind]
}

// StateStore keeps per-user conversation state between updates.
type StateStore interface {
	Get(ctx context.Context, key string) (map[string]any, error)
	Set(ctx context.Context, key string, v map[string]any) error
	Delete(ctx context.Context, key string) error
}

// StateBackendFactory builds a state store; registered by modules.
type StateBackendFactory func(e *Engine) (StateStore, error)

var stateBackends = map[string]struct {
	requires []string
	f        StateBackendFactory
}{}

func RegisterStateBackend(name string, requires []string, f StateBackendFactory) {
	regMu.Lock()
	defer regMu.Unlock()
	stateBackends[name] = struct {
		requires []string
		f        StateBackendFactory
	}{requires, f}
}

// Build is passed to node constructors while compiling a workflow.
type Build struct {
	Workflow *workflow.Workflow
	requires map[string]bool
	state    bool
}

// Compile compiles a parameter value ({{ }} templates allowed).
func (b *Build) Compile(v any) (tmpl.Value, error) { return tmpl.Compile(v) }

// Param compiles params[key], or returns a constant def if absent.
func (b *Build) Param(n workflow.Node, key string, def any) (tmpl.Value, error) {
	v, ok := n.Params[key]
	if !ok {
		return tmpl.Const{V: def}, nil
	}
	c, err := tmpl.Compile(v)
	if err != nil {
		return nil, fmt.Errorf("node %s param %s: %w", n.ID, key, err)
	}
	return c, nil
}

// Condition compiles params[key] as a boolean expression; nil if absent.
func (b *Build) Condition(n workflow.Node, key string) (*tmpl.Expr, error) {
	s, ok := n.Params[key].(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil, nil
	}
	e, err := tmpl.CompileCondition(s)
	if err != nil {
		return nil, fmt.Errorf("node %s param %s: %w", n.ID, key, err)
	}
	return e, nil
}

// UseState tells the engine to load per-user state for every update.
func (b *Build) UseState() { b.state = true }

// Require declares an extra service requirement.
func (b *Build) Require(r string) { b.requires[r] = true }
