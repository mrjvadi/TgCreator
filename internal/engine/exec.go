package engine

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mrjvadi/tgcreator/internal/tg"
	"github.com/mrjvadi/tgcreator/internal/tmpl"
)

// Exec is the execution context of one flow run for one update.
type Exec struct {
	Ctx     context.Context
	E       *Engine
	Update  map[string]any
	Type    string         // update type: message, callback_query, ...
	Env     tmpl.Env       // expression environment
	Vars    map[string]any // flow variables (logic.set)
	Results map[string]any // node id -> output data
	Log     *slog.Logger

	stateKey string
	steps    int   // shared by nested branches (logic.foreach)
	cur      *node // node being executed
	bg       *sync.WaitGroup

	callbackAnswered bool
}

var messageTypes = map[string]bool{
	"message": true, "edited_message": true, "channel_post": true,
	"edited_channel_post": true, "business_message": true, "edited_business_message": true,
	"guest_message": true,
}

// UpdateType returns the payload key of an update ("message", ...).
func UpdateType(u map[string]any) string {
	for k := range u {
		if k != "update_id" {
			return k
		}
	}
	return ""
}

var empty = map[string]any{}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return empty
}

// baseEnv extracts the commonly used objects so expressions can write
// chat.id, from.id, text, command, args... for any update type.
func (e *Engine) baseEnv(u map[string]any, typ string) tmpl.Env {
	payload := asMap(u[typ])
	message := empty
	var callback = empty
	switch {
	case messageTypes[typ]:
		message = payload
	case typ == "callback_query":
		callback = payload
		message = asMap(payload["message"])
	}
	chat := asMap(message["chat"])
	if len(chat) == 0 {
		chat = asMap(payload["chat"])
	}
	from := asMap(payload["from"])
	if len(from) == 0 {
		from = asMap(payload["user"]) // poll_answer, message_reaction...
	}

	var text string
	switch typ {
	case "inline_query", "chosen_inline_result":
		text = tmpl.ToString(payload["query"])
	default:
		if t, ok := message["text"].(string); ok {
			text = t
		} else {
			text = tmpl.ToString(message["caption"])
		}
	}

	command, argsText := "", ""
	if messageTypes[typ] && strings.HasPrefix(text, "/") {
		first, rest, _ := strings.Cut(text, " ")
		cmd, target, hasTarget := strings.Cut(first[1:], "@")
		if !hasTarget || strings.EqualFold(target, e.botUsername) {
			command = strings.ToLower(cmd)
			argsText = strings.TrimSpace(rest)
		}
	}
	args := make([]any, 0, 4)
	for _, a := range strings.Fields(argsText) {
		args = append(args, a)
	}

	return tmpl.Env{
		"update":    u,
		"type":      typ,
		"payload":   payload,
		"message":   message,
		"callback":  callback,
		"data":      callback["data"],
		"chat":      chat,
		"from":      from,
		"text":      text,
		"command":   command,
		"args":      args,
		"args_text": argsText,
		"reply":     asMap(message["reply_to_message"]),
		"bot":       e.botInfo,
		"env":       e.publicEnv,
	}
}

func (e *Engine) newExec(ctx context.Context, u map[string]any, typ string, base tmpl.Env) *Exec {
	env := make(tmpl.Env, len(base)+4)
	for k, v := range base {
		env[k] = v
	}
	vars := make(map[string]any, len(e.globals)+4)
	for k, v := range e.globals {
		vars[k] = v
	}
	results := make(map[string]any, 8)
	env["vars"] = vars
	env["nodes"] = results
	return &Exec{Ctx: ctx, E: e, Update: u, Type: typ, Env: env, Vars: vars, Results: results, Log: e.log}
}

// Eval evaluates a compiled parameter in this execution.
func (x *Exec) Eval(v tmpl.Value) (any, error) { return v.Eval(x.Env) }

// String evaluates v as text.
func (x *Exec) String(v tmpl.Value) (string, error) {
	r, err := v.Eval(x.Env)
	return tmpl.ToString(r), err
}

// TG returns the Telegram client.
func (x *Exec) TG() *tg.Client { return x.E.tg }

// Chat returns the current chat object (may be empty).
func (x *Exec) Chat() map[string]any { return asMap(x.Env["chat"]) }

// From returns the current user object (may be empty).
func (x *Exec) From() map[string]any { return asMap(x.Env["from"]) }

// Message returns the current message (may be empty).
func (x *Exec) Message() map[string]any { return asMap(x.Env["message"]) }

// Callback returns the current callback query (may be empty).
func (x *Exec) Callback() map[string]any { return asMap(x.Env["callback"]) }

// SetVar sets a flow variable visible as vars.<name>.
func (x *Exec) SetVar(name string, v any) { x.Vars[name] = v }

// State returns the current user's state (loaded if the workflow uses it).
func (x *Exec) State() map[string]any {
	if m, ok := x.Env["state"].(map[string]any); ok {
		return m
	}
	return nil
}

// SaveState persists st as the current user's state.
func (x *Exec) SaveState(st map[string]any) error {
	if x.E.state == nil || x.stateKey == "" {
		return nil
	}
	x.Env["state"] = st
	if len(st) == 0 {
		return x.E.state.Delete(x.Ctx, x.stateKey)
	}
	return x.E.state.Set(x.Ctx, x.stateKey, st)
}

// Brancher returns a function that synchronously runs the nodes connected
// to the given output of the node currently executing. Used by nodes that
// run a sub-flow several times (logic.foreach).
func (x *Exec) Brancher() func(output string) {
	n := x.cur
	return func(output string) { x.E.run(x, n.next[output]) }
}

// After runs the nodes connected to output of the current node once d has
// passed, without blocking the worker: other updates of the chat keep
// flowing meanwhile. The delayed steps see a snapshot of vars and results.
func (x *Exec) After(d time.Duration, output string) {
	targets := x.cur.next[output]
	if len(targets) == 0 {
		return
	}
	cp := x.fork()
	e := x.E
	if x.bg != nil {
		x.bg.Add(1)
	}
	e.deferred.Add(1)
	go func() {
		defer func() {
			e.deferred.Done()
			if x.bg != nil {
				x.bg.Done()
			}
		}()
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-t.C:
		case <-e.stop.Done():
			e.log.Warn("delayed step skipped: shutting down", "node", targets[0].spec.ID)
			return
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(x.Ctx), e.timeout)
		defer cancel()
		cp.Ctx = ctx
		e.run(cp, targets)
	}()
}

func (x *Exec) fork() *Exec {
	cp := *x
	cp.Vars = make(map[string]any, len(x.Vars))
	for k, v := range x.Vars {
		cp.Vars[k] = v
	}
	cp.Results = make(map[string]any, len(x.Results))
	for k, v := range x.Results {
		cp.Results[k] = v
	}
	cp.Env = make(map[string]any, len(x.Env))
	for k, v := range x.Env {
		cp.Env[k] = v
	}
	cp.Env["vars"], cp.Env["nodes"] = cp.Vars, cp.Results
	return &cp
}

// MarkCallbackAnswered records that the flow answered the callback query.
func (x *Exec) MarkCallbackAnswered() { x.callbackAnswered = true }
