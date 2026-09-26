// Package tmpl compiles workflow parameters into values that are evaluated
// per update. Strings may embed expressions as {{ expr }}; everything is
// compiled once at load time so the hot path only runs bytecode.
package tmpl

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/vm"
)

// Env is the data an expression can see (update, message, chat, vars, ...).
type Env = map[string]any

// Value is a compiled parameter.
type Value interface {
	Eval(env Env) (any, error)
}

// Const is a Value without any expression inside.
type Const struct{ V any }

func (c Const) Eval(Env) (any, error) { return c.V, nil }

// IsConst reports whether v never depends on the environment.
func IsConst(v Value) bool { _, ok := v.(Const); return ok }

// Expr is a single compiled expression.
type Expr struct {
	src  string
	prog *vm.Program
}

func (e *Expr) Eval(env Env) (any, error) {
	out, err := vm.Run(e.prog, env)
	if err != nil {
		return nil, fmt.Errorf("expression %q: %w", e.src, err)
	}
	return out, nil
}

func (e *Expr) String() string { return e.src }

type concat struct{ parts []Value }

func (c concat) Eval(env Env) (any, error) {
	var b strings.Builder
	for _, p := range c.parts {
		v, err := p.Eval(env)
		if err != nil {
			return nil, err
		}
		b.WriteString(ToString(v))
	}
	return b.String(), nil
}

type object map[string]Value

func (o object) Eval(env Env) (any, error) {
	out := make(map[string]any, len(o))
	for k, v := range o {
		r, err := v.Eval(env)
		if err != nil {
			return nil, err
		}
		out[k] = r
	}
	return out, nil
}

type list []Value

func (l list) Eval(env Env) (any, error) {
	out := make([]any, len(l))
	for i, v := range l {
		r, err := v.Eval(env)
		if err != nil {
			return nil, err
		}
		out[i] = r
	}
	return out, nil
}

// nilSafe turns every a.b into a?.b so missing fields yield nil instead of
// errors; workflows built in a UI should not crash on absent data.
type nilSafe struct{}

func (nilSafe) Visit(n *ast.Node) {
	if m, ok := (*n).(*ast.MemberNode); ok && !m.Optional {
		m.Optional = true
		ast.Patch(n, &ast.ChainNode{Node: m})
	}
}

// CompileExpr compiles a bare expression (no {{ }}), e.g. a condition.
func CompileExpr(src string) (*Expr, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, fmt.Errorf("empty expression")
	}
	opts := append([]expr.Option{
		expr.Env(map[string]any{}),
		expr.AllowUndefinedVariables(),
		expr.Patch(nilSafe{}),
	}, functions...)
	prog, err := expr.Compile(src, opts...)
	if err != nil {
		return nil, fmt.Errorf("compile %q: %w", src, err)
	}
	return &Expr{src: src, prog: prog}, nil
}

// CompileCondition accepts either a bare expression or one wrapped in {{ }}.
func CompileCondition(src string) (*Expr, error) {
	s := strings.TrimSpace(src)
	if strings.HasPrefix(s, "{{") && strings.HasSuffix(s, "}}") && strings.Count(s, "{{") == 1 {
		s = s[2 : len(s)-2]
	}
	return CompileExpr(s)
}

// Compile turns a decoded JSON parameter into a Value.
func Compile(v any) (Value, error) {
	switch t := v.(type) {
	case string:
		return compileString(t)
	case map[string]any:
		o := make(object, len(t))
		allConst := true
		for k, x := range t {
			c, err := Compile(x)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			o[k] = c
			allConst = allConst && IsConst(c)
		}
		if allConst {
			return Const{V: t}, nil
		}
		return o, nil
	case []any:
		l := make(list, len(t))
		allConst := true
		for i, x := range t {
			c, err := Compile(x)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			l[i] = c
			allConst = allConst && IsConst(c)
		}
		if allConst {
			return Const{V: t}, nil
		}
		return l, nil
	default:
		return Const{V: v}, nil
	}
}

func compileString(s string) (Value, error) {
	if !strings.Contains(s, "{{") {
		return Const{V: s}, nil
	}
	var parts []Value
	rest := s
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			if rest != "" {
				parts = append(parts, Const{V: rest})
			}
			break
		}
		j := strings.Index(rest[i+2:], "}}")
		if j < 0 {
			return nil, fmt.Errorf("unclosed {{ in %q", s)
		}
		if i > 0 {
			parts = append(parts, Const{V: rest[:i]})
		}
		e, err := CompileExpr(rest[i+2 : i+2+j])
		if err != nil {
			return nil, err
		}
		parts = append(parts, e)
		rest = rest[i+2+j+2:]
	}
	// "{{ expr }}" alone keeps the raw type (number, list, object...).
	if len(parts) == 1 {
		return parts[0], nil
	}
	return concat{parts: parts}, nil
}

// ToString renders a value for text output. Numbers never use exponent
// notation so Telegram ids stay intact.
func ToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return t.String()
	case []byte:
		return string(t)
	case fmt.Stringer:
		return t.String()
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}

// Truthy is the boolean interpretation used by conditions.
func Truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != "" && t != "0" && t != "false"
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	default:
		return true
	}
}

// ToInt64 converts numeric-ish values (float64 from JSON, strings) to int64.
func ToInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case int:
		return int64(t), true
	case int64:
		return t, true
	case json.Number:
		n, err := t.Int64()
		return n, err == nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return n, err == nil
	}
	return 0, false
}
