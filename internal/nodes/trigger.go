// Package nodes contains the built-in node types. Each file registers its
// nodes in init(); optional modules (redis, sql) are guarded by build tags
// so they can be left out of the binary entirely.
package nodes

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mrjvadi/tgcreator/internal/engine"
	"github.com/mrjvadi/tgcreator/internal/tmpl"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

func init() {
	engine.Register(engine.NodeType{
		Name:        "trigger.message",
		Description: "New message. Filters: chat_types, has (photo, video, document, sticker, new_chat_members...), text, contains, regex, condition. updates: [message, edited_message, channel_post...]",
		New:         newMessageTrigger,
	})
	engine.Register(engine.NodeType{
		Name:        "trigger.command",
		Description: "Bot command like /start. Params: commands, chat_types, condition. Sets command, args, args_text.",
		New:         newCommandTrigger,
	})
	engine.Register(engine.NodeType{
		Name:        "trigger.callback",
		Description: "Inline button press. Params: data (exact), prefix, regex, condition. Output: {data, suffix, matches}.",
		New:         newCallbackTrigger,
	})
	engine.Register(engine.NodeType{
		Name:        "trigger.update",
		Description: "Any update type. Params: on (list of update types or \"*\"), condition.",
		New:         newUpdateTrigger,
	})
}

// common filters shared by triggers.
type filter struct {
	chatTypes map[string]bool
	cond      *tmpl.Expr
}

func newFilter(b *engine.Build, n workflow.Node) (filter, error) {
	f := filter{}
	if ct := stringList(n.Params["chat_types"]); len(ct) > 0 {
		f.chatTypes = map[string]bool{}
		for _, t := range ct {
			f.chatTypes[t] = true
		}
	}
	var err error
	f.cond, err = b.Condition(n, "condition")
	return f, err
}

func (f filter) ok(x *engine.Exec) bool {
	if f.chatTypes != nil && !f.chatTypes[tmpl.ToString(x.Chat()["type"])] {
		return false
	}
	if f.cond != nil {
		v, err := f.cond.Eval(x.Env)
		if err != nil {
			x.Log.Warn("trigger condition failed", "err", err)
			return false
		}
		return tmpl.Truthy(v)
	}
	return true
}

type messageTrigger struct {
	filter
	updates  []string
	has      []string
	text     string
	contains []string
	re       *regexp.Regexp
}

func newMessageTrigger(b *engine.Build, n workflow.Node) (any, error) {
	f, err := newFilter(b, n)
	if err != nil {
		return nil, err
	}
	t := &messageTrigger{
		filter:   f,
		updates:  stringList(n.Params["updates"]),
		has:      stringList(n.Params["has"]),
		text:     tmpl.ToString(n.Params["text"]),
		contains: stringList(n.Params["contains"]),
	}
	if len(t.updates) == 0 {
		t.updates = []string{"message"}
	}
	if r := tmpl.ToString(n.Params["regex"]); r != "" {
		if t.re, err = regexp.Compile(r); err != nil {
			return nil, fmt.Errorf("regex: %w", err)
		}
	}
	return t, nil
}

func (t *messageTrigger) UpdateTypes() []string { return t.updates }

func (t *messageTrigger) Match(x *engine.Exec) (any, bool) {
	msg := x.Message()
	if len(t.has) > 0 {
		found := false
		for _, h := range t.has {
			if _, ok := msg[h]; ok {
				found = true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	text, _ := x.Env["text"].(string)
	if t.text != "" && text != t.text {
		return nil, false
	}
	if len(t.contains) > 0 {
		lower := strings.ToLower(text)
		found := false
		for _, c := range t.contains {
			if strings.Contains(lower, strings.ToLower(c)) {
				found = true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	var matches []any
	if t.re != nil {
		m := t.re.FindStringSubmatch(text)
		if m == nil {
			return nil, false
		}
		matches = toAnyList(m)
	}
	if !t.ok(x) {
		return nil, false
	}
	return map[string]any{"text": text, "matches": matches}, true
}

type commandTrigger struct {
	filter
	commands map[string]bool
	updates  []string
}

func newCommandTrigger(b *engine.Build, n workflow.Node) (any, error) {
	f, err := newFilter(b, n)
	if err != nil {
		return nil, err
	}
	cmds := stringList(n.Params["commands"])
	if c := tmpl.ToString(n.Params["command"]); c != "" {
		cmds = append(cmds, c)
	}
	if len(cmds) == 0 {
		return nil, fmt.Errorf("commands is required")
	}
	t := &commandTrigger{filter: f, commands: map[string]bool{}, updates: stringList(n.Params["updates"])}
	for _, c := range cmds {
		t.commands[strings.ToLower(strings.TrimPrefix(c, "/"))] = true
	}
	if len(t.updates) == 0 {
		t.updates = []string{"message"}
	}
	return t, nil
}

func (t *commandTrigger) UpdateTypes() []string { return t.updates }

func (t *commandTrigger) Match(x *engine.Exec) (any, bool) {
	cmd, _ := x.Env["command"].(string)
	if cmd == "" || !t.commands[cmd] || !t.ok(x) {
		return nil, false
	}
	return map[string]any{"command": cmd, "args": x.Env["args"], "args_text": x.Env["args_text"]}, true
}

type callbackTrigger struct {
	filter
	data, prefix string
	re           *regexp.Regexp
}

func newCallbackTrigger(b *engine.Build, n workflow.Node) (any, error) {
	f, err := newFilter(b, n)
	if err != nil {
		return nil, err
	}
	t := &callbackTrigger{filter: f, data: tmpl.ToString(n.Params["data"]), prefix: tmpl.ToString(n.Params["prefix"])}
	if r := tmpl.ToString(n.Params["regex"]); r != "" {
		if t.re, err = regexp.Compile(r); err != nil {
			return nil, fmt.Errorf("regex: %w", err)
		}
	}
	return t, nil
}

func (t *callbackTrigger) UpdateTypes() []string { return []string{"callback_query"} }

func (t *callbackTrigger) Match(x *engine.Exec) (any, bool) {
	data := tmpl.ToString(x.Callback()["data"])
	if t.data != "" && data != t.data {
		return nil, false
	}
	if t.prefix != "" && !strings.HasPrefix(data, t.prefix) {
		return nil, false
	}
	var matches []any
	if t.re != nil {
		m := t.re.FindStringSubmatch(data)
		if m == nil {
			return nil, false
		}
		matches = toAnyList(m)
	}
	if !t.ok(x) {
		return nil, false
	}
	return map[string]any{"data": data, "suffix": strings.TrimPrefix(data, t.prefix), "matches": matches}, true
}

type updateTrigger struct {
	filter
	on []string
}

func newUpdateTrigger(b *engine.Build, n workflow.Node) (any, error) {
	f, err := newFilter(b, n)
	if err != nil {
		return nil, err
	}
	on := stringList(n.Params["on"])
	if len(on) == 0 {
		return nil, fmt.Errorf("on is required (update types or \"*\")")
	}
	return &updateTrigger{filter: f, on: on}, nil
}

func (t *updateTrigger) UpdateTypes() []string { return t.on }

func (t *updateTrigger) Match(x *engine.Exec) (any, bool) {
	if !t.ok(x) {
		return nil, false
	}
	return x.Env["payload"], true
}

// stringList accepts a string or a list of strings.
func stringList(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, s := range t {
			out = append(out, tmpl.ToString(s))
		}
		return out
	case []string:
		return t
	}
	return nil
}

func toAnyList(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}
