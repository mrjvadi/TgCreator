package tmpl

import (
	"strings"
	"testing"
)

func eval(t *testing.T, v any, env Env) any {
	t.Helper()
	c, err := Compile(v)
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Eval(env)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCompile(t *testing.T) {
	env := Env{
		"chat":    map[string]any{"id": -1001234567890.0},
		"from":    map[string]any{"id": 42.0, "first_name": "Ali <3"},
		"args":    []any{},
		"message": map[string]any{"text": "see https://x.com", "entities": []any{map[string]any{"type": "url"}}},
	}
	tests := []struct {
		in   any
		want any
	}{
		{"plain", "plain"},
		{"{{ chat.id }}", -1001234567890.0},
		{"id={{ chat.id }}", "id=-1001234567890"},
		{"{{ message.reply_to_message.from.id }}", nil},
		{"hi {{ from.first_name }}!", "hi Ali <3!"},
		{"{{ mention(from) }}", `<a href="tg://user?id=42">Ali &lt;3</a>`},
		{"{{ hasLink(message) }}", true},
		{"{{ hasLink('hello') }}", false},
		{"{{ upper(from.first_name) }}", "ALI <3"},
		{"{{ args[0] ?? 'none' }}", "none"},
		{"{{ message.entities[0].type }}", "url"},
		{"{{ message.entities[5].type }}", nil},
		{"{{ message.entities[-1].type }}", "url"},
		{"{{ from['first_name'] }}", "Ali <3"},
	}
	for _, tt := range tests {
		if got := eval(t, tt.in, env); got != tt.want {
			t.Errorf("%v: got %#v want %#v", tt.in, got, tt.want)
		}
	}

	obj := eval(t, map[string]any{"a": "{{ from.id }}", "b": []any{"x", "{{ 1 + 1 }}"}}, env).(map[string]any)
	if obj["a"] != 42.0 || obj["b"].([]any)[1] != 2 {
		t.Errorf("object: %#v", obj)
	}
}

func TestConstFolding(t *testing.T) {
	c, err := Compile(map[string]any{"a": []any{"x", 1.0}})
	if err != nil {
		t.Fatal(err)
	}
	if !IsConst(c) {
		t.Fatal("expected constant")
	}
}

func TestCondition(t *testing.T) {
	for _, src := range []string{`chat.type == "group"`, `{{ chat.type == "group" }}`} {
		e, err := CompileCondition(src)
		if err != nil {
			t.Fatal(err)
		}
		v, _ := e.Eval(Env{"chat": map[string]any{"type": "group"}})
		if !Truthy(v) {
			t.Errorf("%s: want true", src)
		}
	}
}

// randomString is evaluated on every run, never folded into a constant.
func TestRandomString(t *testing.T) {
	c, err := Compile("{{ randomString(10) }}")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := c.Eval(Env{})
	b, _ := c.Eval(Env{})
	if IsConst(c) || len(ToString(a)) != 10 || a == b {
		t.Fatalf("got %v and %v", a, b)
	}
	d, _ := Compile(`{{ randomString(6, "0123456789") }}`)
	v, _ := d.Eval(Env{})
	if s := ToString(v); len(s) != 6 || strings.Trim(s, "0123456789") != "" {
		t.Fatalf("digits: %q", s)
	}
}
