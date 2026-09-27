package nodes

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mrjvadi/tgcreator/internal/engine"
	"github.com/mrjvadi/tgcreator/internal/tmpl"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// Telegram nodes pass every parameter they do not understand straight to
// the Bot API, so any option Telegram offers (message_thread_id,
// link_preview_options, protect_content, effects, ...) works without code.
//
// Helpers understood by all send/edit nodes:
//   buttons:  [[{text, callback_data|url|web_app|...}]]  -> inline keyboard
//   keyboard: [["A", "B"], [{text, request_contact}]]    -> reply keyboard
//   remove_keyboard: true
//   reply: true   -> reply to the triggering message
// chat_id defaults to the current chat.

var mediaTypes = map[string]string{
	"photo": "sendPhoto", "video": "sendVideo", "audio": "sendAudio",
	"document": "sendDocument", "animation": "sendAnimation", "voice": "sendVoice",
	"video_note": "sendVideoNote", "sticker": "sendSticker",
}

func init() {
	engine.Register(engine.NodeType{
		Name:        "telegram.send_message",
		Description: "Send text. Params: text, parse_mode, buttons, keyboard, reply, chat_id + any sendMessage option.",
		New:         apiNode("sendMessage", defaultChat, defaultParseMode),
	})
	engine.Register(engine.NodeType{
		Name:        "telegram.send_media",
		Description: "Send photo/video/audio/document/animation/voice/video_note/sticker. Params: type, file (file_id, URL or file:///path), caption, buttons...",
		New:         newSendMedia,
	})
	engine.Register(engine.NodeType{
		Name:        "telegram.send_media_group",
		Description: "Send an album. Params: media (list of InputMedia; media may be file:///path).",
		New:         apiNode("sendMediaGroup", defaultChat),
	})
	engine.Register(engine.NodeType{
		Name:        "telegram.edit_message",
		Description: "Edit text of a message (defaults to the message of the pressed button). Params: text, buttons...",
		New:         apiNode("editMessageText", defaultChat, defaultMessageID, defaultParseMode),
	})
	engine.Register(engine.NodeType{
		Name:        "telegram.edit_buttons",
		Description: "Replace the inline keyboard of a message. Params: buttons.",
		New:         apiNode("editMessageReplyMarkup", defaultChat, defaultMessageID),
	})
	engine.Register(engine.NodeType{
		Name:        "telegram.delete_message",
		Description: "Delete a message (defaults to the triggering message).",
		New:         apiNode("deleteMessage", defaultChat, defaultMessageID),
	})
	engine.Register(engine.NodeType{
		Name:        "telegram.answer_callback",
		Description: "Answer a button press. Params: text, show_alert, url.",
		New:         apiNode("answerCallbackQuery", defaultCallbackID),
	})
	engine.Register(engine.NodeType{
		Name:        "telegram.api",
		Description: "Call any Bot API method. Params: method, params.",
		New:         newGenericAPI,
	})
	engine.Register(engine.NodeType{
		Name:        "tg.",
		Description: "tg.<method>: call any Bot API method directly, e.g. tg.banChatMember with params as the method's fields.",
		New: func(b *engine.Build, n workflow.Node) (any, error) {
			return apiNode(strings.TrimPrefix(n.Type, "tg."))(b, n)
		},
	})
	engine.Register(engine.NodeType{
		Name:        "telegram.check_admin",
		Description: "Is a user admin/creator of the chat? Cached. Params: user_id, chat_id, cache (\"5m\"). Outputs: true, false.",
		New:         newCheckAdmin,
	})
}

// A defaulter fills a missing parameter from the current update.
type defaulter func(x *engine.Exec, p map[string]any)

func defaultChat(x *engine.Exec, p map[string]any) {
	if _, ok := p["chat_id"]; !ok {
		if _, inline := p["inline_message_id"]; !inline {
			p["chat_id"] = x.Chat()["id"]
		}
	}
}

func defaultMessageID(x *engine.Exec, p map[string]any) {
	if _, ok := p["message_id"]; !ok {
		if _, inline := p["inline_message_id"]; inline {
			return
		}
		p["message_id"] = x.Message()["message_id"]
	}
}

func defaultCallbackID(x *engine.Exec, p map[string]any) {
	if _, ok := p["callback_query_id"]; !ok {
		p["callback_query_id"] = x.Callback()["id"]
	}
}

func defaultParseMode(x *engine.Exec, p map[string]any) {
	// Entities carry the formatting themselves; Telegram rejects both.
	if p["entities"] != nil || p["caption_entities"] != nil {
		if _, ok := p["parse_mode"]; !ok {
			return
		}
	}
	if _, ok := p["parse_mode"]; !ok {
		if pm := x.E.WF.Bot.ParseMode; pm != "" {
			p["parse_mode"] = pm
		}
	}
	if p["parse_mode"] == "" {
		delete(p, "parse_mode")
	}
}

type apiCall struct {
	method   string
	params   tmpl.Value
	defaults []defaulter
}

func apiNode(method string, defaults ...defaulter) func(*engine.Build, workflow.Node) (any, error) {
	return func(b *engine.Build, n workflow.Node) (any, error) {
		p, err := b.Compile(n.Params)
		if err != nil {
			return nil, err
		}
		return &apiCall{method: method, params: p, defaults: defaults}, nil
	}
}

func (a *apiCall) Exec(x *engine.Exec) (engine.Result, error) {
	v, err := x.Eval(a.params)
	if err != nil {
		return engine.Result{}, err
	}
	p := cloneMap(v)
	return callAPI(x, a.method, p, a.defaults)
}

func callAPI(x *engine.Exec, method string, p map[string]any, defaults []defaulter) (engine.Result, error) {
	for _, d := range defaults {
		d(x, p)
	}
	applyHelpers(x, p)
	// nil means "not set": {{ cond ? value : nil }} leaves a parameter out.
	for k, v := range p {
		if v == nil {
			delete(p, k)
		}
	}
	raw, err := x.TG().Call(x.Ctx, method, p)
	if err != nil {
		return engine.Result{}, err
	}
	if method == "answerCallbackQuery" {
		x.MarkCallbackAnswered()
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return engine.Result{}, err
	}
	return engine.Result{Output: engine.Main, Data: out}, nil
}

func applyHelpers(x *engine.Exec, p map[string]any) {
	if rows, ok := p["buttons"]; ok {
		delete(p, "buttons")
		if kb := buttonRows(rows, false); len(kb) > 0 {
			p["reply_markup"] = map[string]any{"inline_keyboard": kb}
		}
	}
	if rows, ok := p["keyboard"]; ok {
		delete(p, "keyboard")
		if kb := buttonRows(rows, true); len(kb) > 0 {
			p["reply_markup"] = map[string]any{"keyboard": kb, "resize_keyboard": true}
		}
	}
	if rm, ok := p["remove_keyboard"]; ok {
		delete(p, "remove_keyboard")
		if tmpl.Truthy(rm) {
			p["reply_markup"] = map[string]any{"remove_keyboard": true}
		}
	}
	if r, ok := p["reply"]; ok {
		delete(p, "reply")
		if tmpl.Truthy(r) {
			if id := x.Message()["message_id"]; id != nil {
				p["reply_parameters"] = map[string]any{"message_id": id, "allow_sending_without_reply": true}
			}
		}
	}
}

// buttonRows normalizes [[btn]] or [btn] (one button per row). Strings are
// allowed for reply keyboards; rows or buttons that evaluate to null/empty
// are dropped so a UI can hide buttons conditionally.
func buttonRows(v any, allowString bool) []any {
	list, _ := v.([]any)
	rows := make([]any, 0, len(list))
	for _, r := range list {
		var row []any
		if inner, ok := r.([]any); ok {
			row = inner
		} else {
			row = []any{r}
		}
		clean := make([]any, 0, len(row))
		for _, b := range row {
			switch t := b.(type) {
			case map[string]any:
				if tmpl.ToString(t["text"]) != "" {
					clean = append(clean, t)
				}
			case string:
				if allowString && t != "" {
					clean = append(clean, t)
				}
			}
		}
		if len(clean) > 0 {
			rows = append(rows, clean)
		}
	}
	return rows
}

type sendMedia struct {
	params tmpl.Value
}

func newSendMedia(b *engine.Build, n workflow.Node) (any, error) {
	if t, ok := n.Params["type"].(string); ok && !strings.Contains(t, "{{") {
		if _, known := mediaTypes[t]; !known {
			return nil, fmt.Errorf("unknown media type %q", t)
		}
	}
	p, err := b.Compile(n.Params)
	if err != nil {
		return nil, err
	}
	return &sendMedia{params: p}, nil
}

func (s *sendMedia) Exec(x *engine.Exec) (engine.Result, error) {
	v, err := x.Eval(s.params)
	if err != nil {
		return engine.Result{}, err
	}
	p := cloneMap(v)
	typ := tmpl.ToString(p["type"])
	method, ok := mediaTypes[typ]
	if !ok {
		return engine.Result{}, fmt.Errorf("unknown media type %q", typ)
	}
	delete(p, "type")
	if f, ok := p["file"]; ok {
		delete(p, "file")
		p[typ] = f
	}
	return callAPI(x, method, p, []defaulter{defaultChat, defaultParseMode})
}

type genericAPI struct {
	method tmpl.Value
	params tmpl.Value
}

func newGenericAPI(b *engine.Build, n workflow.Node) (any, error) {
	m, err := b.Param(n, "method", "")
	if err != nil {
		return nil, err
	}
	p, err := b.Param(n, "params", map[string]any{})
	if err != nil {
		return nil, err
	}
	return &genericAPI{method: m, params: p}, nil
}

func (g *genericAPI) Exec(x *engine.Exec) (engine.Result, error) {
	m, err := x.String(g.method)
	if err != nil {
		return engine.Result{}, err
	}
	if m == "" {
		return engine.Result{}, fmt.Errorf("method is empty")
	}
	v, err := x.Eval(g.params)
	if err != nil {
		return engine.Result{}, err
	}
	return callAPI(x, m, cloneMap(v), nil)
}

// Admin checks are frequent in group bots (every locked message), so the
// result is cached to avoid one API call per message.
type checkAdmin struct {
	user, chat tmpl.Value
	ttl        time.Duration
}

type adminEntry struct {
	admin   bool
	fetched time.Time
}

// adminCache is shared by all check_admin nodes of the process.
var adminCache = struct {
	sync.Mutex
	m map[string]adminEntry
}{m: map[string]adminEntry{}}

func newCheckAdmin(b *engine.Build, n workflow.Node) (any, error) {
	user, err := b.Compile(paramOr(n, "user_id", "{{ from.id }}"))
	if err != nil {
		return nil, err
	}
	chat, err := b.Compile(paramOr(n, "chat_id", "{{ chat.id }}"))
	if err != nil {
		return nil, err
	}
	ttl := 5 * time.Minute
	if s, ok := n.Params["cache"].(string); ok && s != "" {
		if ttl, err = time.ParseDuration(s); err != nil {
			return nil, fmt.Errorf("cache: %w", err)
		}
	}
	return &checkAdmin{user: user, chat: chat, ttl: ttl}, nil
}

func (c *checkAdmin) Exec(x *engine.Exec) (engine.Result, error) {
	user, err := x.Eval(c.user)
	if err != nil {
		return engine.Result{}, err
	}
	chat, err := x.Eval(c.chat)
	if err != nil {
		return engine.Result{}, err
	}
	key := tmpl.ToString(chat) + ":" + tmpl.ToString(user)
	now := time.Now()
	adminCache.Lock()
	en, ok := adminCache.m[key]
	adminCache.Unlock()
	if !ok || now.Sub(en.fetched) > c.ttl {
		var member map[string]any
		if err := x.TG().CallInto(x.Ctx, "getChatMember", map[string]any{"chat_id": chat, "user_id": user}, &member); err != nil {
			return engine.Result{}, err
		}
		st := tmpl.ToString(member["status"])
		en = adminEntry{admin: st == "administrator" || st == "creator", fetched: now}
		adminCache.Lock()
		if len(adminCache.m) > 100_000 {
			adminCache.m = map[string]adminEntry{}
		}
		adminCache.m[key] = en
		adminCache.Unlock()
	}
	// Anonymous admins post as the group itself.
	if sc, ok := x.Message()["sender_chat"].(map[string]any); ok && !en.admin &&
		tmpl.ToString(sc["id"]) == tmpl.ToString(chat) && tmpl.ToString(user) == tmpl.ToString(x.From()["id"]) {
		en.admin = true
	}
	out := "false"
	if en.admin {
		out = "true"
	}
	return engine.Result{Output: out, Data: en.admin}, nil
}

func paramOr(n workflow.Node, key string, def any) any {
	if v, ok := n.Params[key]; ok {
		return v
	}
	return def
}

func cloneMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	out := make(map[string]any, len(m)+2)
	for k, val := range m {
		out[k] = val
	}
	return out
}
