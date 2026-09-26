package nodes

import (
	"fmt"
	"time"

	"github.com/mrjvadi/tgcreator/internal/engine"
	"github.com/mrjvadi/tgcreator/internal/tmpl"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

func init() {
	engine.Register(engine.NodeType{
		Name:        "logic.if",
		Description: "Branch on a condition. Outputs: true, false.",
		New: func(b *engine.Build, n workflow.Node) (any, error) {
			c, err := b.Condition(n, "condition")
			if err != nil {
				return nil, err
			}
			if c == nil {
				return nil, fmt.Errorf("condition is required")
			}
			return &ifNode{cond: c}, nil
		},
	})
	engine.Register(engine.NodeType{
		Name:        "logic.switch",
		Description: "Route by value. Params: value, cases (list of strings). Output: the matching case or \"default\".",
		New: func(b *engine.Build, n workflow.Node) (any, error) {
			v, err := b.Param(n, "value", nil)
			if err != nil {
				return nil, err
			}
			cases := map[string]bool{}
			for _, c := range stringList(n.Params["cases"]) {
				cases[c] = true
			}
			return &switchNode{value: v, cases: cases}, nil
		},
	})
	engine.Register(engine.NodeType{
		Name:        "logic.set",
		Description: "Set flow variables. Params: vars {name: value}. Readable as vars.<name>.",
		New: func(b *engine.Build, n workflow.Node) (any, error) {
			vars, _ := n.Params["vars"].(map[string]any)
			s := &setNode{vars: map[string]tmpl.Value{}}
			for k, v := range vars {
				c, err := b.Compile(v)
				if err != nil {
					return nil, fmt.Errorf("vars.%s: %w", k, err)
				}
				s.vars[k] = c
			}
			return s, nil
		},
	})
	engine.Register(engine.NodeType{
		Name:        "logic.foreach",
		Description: "Run the \"item\" output once per element of items (vars.item, vars.index), then \"done\". Params: items, delay (between items, e.g. \"40ms\" for broadcasts).",
		New: func(b *engine.Build, n workflow.Node) (any, error) {
			items, err := b.Param(n, "items", []any{})
			if err != nil {
				return nil, err
			}
			var delay time.Duration
			if s, ok := n.Params["delay"].(string); ok && s != "" {
				if delay, err = time.ParseDuration(s); err != nil {
					return nil, fmt.Errorf("delay: %w", err)
				}
			}
			return &foreachNode{items: items, delay: delay}, nil
		},
	})
	engine.Register(engine.NodeType{
		Name:        "logic.delay",
		Description: "Continue the branch after a pause without blocking other updates. Params: duration (\"10s\", \"200ms\").",
		New: func(b *engine.Build, n workflow.Node) (any, error) {
			d, err := b.Param(n, "duration", "1s")
			return &delayNode{d: d}, err
		},
	})
	engine.Register(engine.NodeType{
		Name:        "logic.stop",
		Description: "Stop this branch.",
		New: func(*engine.Build, workflow.Node) (any, error) {
			return actionFunc(func(*engine.Exec) (engine.Result, error) { return engine.Result{}, nil }), nil
		},
	})
	engine.Register(engine.NodeType{
		Name:        "logic.log",
		Description: "Write a log line. Params: message.",
		New: func(b *engine.Build, n workflow.Node) (any, error) {
			m, err := b.Param(n, "message", "")
			if err != nil {
				return nil, err
			}
			id := n.ID
			return actionFunc(func(x *engine.Exec) (engine.Result, error) {
				s, err := x.String(m)
				x.Log.Info(s, "node", id)
				return engine.Result{Output: engine.Main}, err
			}), nil
		},
	})
	engine.Register(engine.NodeType{
		Name:        "state.set",
		Description: "Merge values into the current user's state (readable as state.<key>). Params: values, clear (bool).",
		New: func(b *engine.Build, n workflow.Node) (any, error) {
			b.UseState()
			values, err := b.Param(n, "values", map[string]any{})
			if err != nil {
				return nil, err
			}
			clear, _ := n.Params["clear"].(bool)
			return &stateSet{values: values, clear: clear}, nil
		},
	})
	engine.Register(engine.NodeType{
		Name:        "state.clear",
		Description: "Delete the current user's state.",
		New: func(b *engine.Build, n workflow.Node) (any, error) {
			b.UseState()
			return &stateSet{values: tmpl.Const{V: map[string]any{}}, clear: true}, nil
		},
	})
}

type actionFunc func(x *engine.Exec) (engine.Result, error)

func (f actionFunc) Exec(x *engine.Exec) (engine.Result, error) { return f(x) }

type ifNode struct{ cond *tmpl.Expr }

func (n *ifNode) Exec(x *engine.Exec) (engine.Result, error) {
	v, err := n.cond.Eval(x.Env)
	if err != nil {
		return engine.Result{}, err
	}
	ok := tmpl.Truthy(v)
	out := "false"
	if ok {
		out = "true"
	}
	return engine.Result{Output: out, Data: ok}, nil
}

type switchNode struct {
	value tmpl.Value
	cases map[string]bool
}

func (n *switchNode) Exec(x *engine.Exec) (engine.Result, error) {
	v, err := x.String(n.value)
	if err != nil {
		return engine.Result{}, err
	}
	if n.cases[v] {
		return engine.Result{Output: v, Data: v}, nil
	}
	return engine.Result{Output: "default", Data: v}, nil
}

type setNode struct{ vars map[string]tmpl.Value }

func (n *setNode) Exec(x *engine.Exec) (engine.Result, error) {
	for k, v := range n.vars {
		r, err := x.Eval(v)
		if err != nil {
			return engine.Result{}, err
		}
		x.SetVar(k, r)
	}
	return engine.Result{Output: engine.Main}, nil
}

type delayNode struct{ d tmpl.Value }

func (n *delayNode) Exec(x *engine.Exec) (engine.Result, error) {
	s, err := x.String(n.d)
	if err != nil {
		return engine.Result{}, err
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return engine.Result{}, err
	}
	// The rest of the branch runs later; the worker is free meanwhile.
	x.After(d, engine.Main)
	return engine.Result{Data: map[string]any{"delayed": s}}, nil
}

type stateSet struct {
	values tmpl.Value
	clear  bool
}

func (n *stateSet) Exec(x *engine.Exec) (engine.Result, error) {
	v, err := x.Eval(n.values)
	if err != nil {
		return engine.Result{}, err
	}
	st := map[string]any{}
	if !n.clear {
		for k, val := range x.State() {
			st[k] = val
		}
	}
	m, _ := v.(map[string]any)
	for k, val := range m {
		if val == nil {
			delete(st, k)
		} else {
			st[k] = val
		}
	}
	return engine.Result{Output: engine.Main, Data: st}, x.SaveState(st)
}

type foreachNode struct {
	items tmpl.Value
	delay time.Duration
}

func (n *foreachNode) Exec(x *engine.Exec) (engine.Result, error) {
	v, err := x.Eval(n.items)
	if err != nil {
		return engine.Result{}, err
	}
	list, ok := v.([]any)
	if !ok && v != nil {
		return engine.Result{}, fmt.Errorf("items must be a list, got %T", v)
	}
	branch := x.Brancher()
	for i, item := range list {
		if i > 0 && n.delay > 0 {
			select {
			case <-time.After(n.delay):
			case <-x.Ctx.Done():
				return engine.Result{}, x.Ctx.Err()
			}
		}
		x.SetVar("item", item)
		x.SetVar("index", i)
		branch("item")
	}
	return engine.Result{Output: "done", Data: map[string]any{"count": len(list)}}, nil
}
