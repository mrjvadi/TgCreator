package tgsim

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type User struct {
	ID        int64
	FirstName string
	Username  string
	IsBot     bool
}

type Chat struct {
	ID       int64
	Type     string // private, group, supergroup, channel
	Title    string
	Username string
}

type Member struct {
	Status string // creator, administrator, member, restricted, left, kicked
	Rights map[string]bool
	Perms  map[string]any
	Until  int64
}

type Msg struct {
	ID         int64
	Chat       *Chat
	From       *User
	Text       string
	Caption    string
	Media      string // photo, video, ... ("" = text)
	Markup     map[string]any
	ByBot      bool
	Deleted    bool
	Pinned     bool
	Reactions  []string
	Extra      map[string]any // contact, dice, poll, invoice, new_chat_members...
	ReplyTo    int64
	SenderChat *Chat
}

// Call is one request made by the bot.
type Call struct {
	Method string
	Params map[string]any
	Error  string
}

// Event is one line of the human-readable transcript.
type Event struct {
	At     time.Time
	ChatID int64
	Chat   string
	Who    string
	Text   string
	Kind   string // user, bot, action, error, filtered
}

type query struct {
	answered bool
	user     int64
	chat     int64
	msg      int64
}

// Sim is a fake Telegram. Point tg.New(sim.Token, sim.URL) at it.
type Sim struct {
	Spec  *Spec
	Token string
	URL   string
	// ValidateOnly accepts every valid call without simulating it.
	ValidateOnly bool

	srv *httptest.Server
	mu  sync.Mutex
	bot *User

	users   map[int64]*User
	chats   map[int64]*Chat
	members map[int64]map[int64]*Member
	msgs    map[int64]map[int64]*Msg
	lastID  map[int64]int64
	started map[int64]bool
	blocked map[int64]bool

	queue      []map[string]any
	nextUpdate int64
	allowed    map[string]bool
	wake       chan struct{}
	delivered  map[int64]bool
	handled    int
	webhook    *webhookCfg

	callbacks map[string]*query
	inline    map[string]*query
	checkouts map[string]*query
	joinReqs  map[[2]int64]bool
	polls     map[string]int64
	seq       int

	Violations []string
	Calls      []Call
	Events     []Event
}

type webhookCfg struct {
	url, secret string
	ch          chan map[string]any
}

// New starts a simulator for a bot with the given username.
func New(botUsername string) *Sim {
	s := &Sim{
		Spec:       LoadSpec(),
		Token:      "999:SIMULATED",
		bot:        &User{ID: 999, FirstName: "Gopher Helper", Username: botUsername, IsBot: true},
		users:      map[int64]*User{},
		chats:      map[int64]*Chat{},
		members:    map[int64]map[int64]*Member{},
		msgs:       map[int64]map[int64]*Msg{},
		lastID:     map[int64]int64{},
		started:    map[int64]bool{},
		blocked:    map[int64]bool{},
		wake:       make(chan struct{}),
		delivered:  map[int64]bool{},
		callbacks:  map[string]*query{},
		inline:     map[string]*query{},
		checkouts:  map[string]*query{},
		joinReqs:   map[[2]int64]bool{},
		polls:      map[string]int64{},
		nextUpdate: 1000,
	}
	s.users[s.bot.ID] = s.bot
	s.srv = httptest.NewServer(s)
	s.URL = s.srv.URL
	return s
}

func (s *Sim) Close() {
	s.srv.CloseClientConnections()
	s.srv.Close()
}

// ---- world setup ----

func (s *Sim) AddUser(id int64, first, username string) *User {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := &User{ID: id, FirstName: first, Username: username}
	s.users[id] = u
	s.chats[id] = &Chat{ID: id, Type: "private", Title: first, Username: username}
	return u
}

func (s *Sim) AddChat(id int64, typ, title, username string) *Chat {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := &Chat{ID: id, Type: typ, Title: title, Username: username}
	s.chats[id] = c
	s.members[id] = map[int64]*Member{}
	return c
}

// AdminRights is the usual set of rights of a group admin bot.
var AdminRights = []string{"can_delete_messages", "can_restrict_members", "can_pin_messages", "can_invite_users", "can_post_messages", "can_edit_messages", "can_manage_chat"}

func (s *Sim) SetMember(chat, user int64, status string, rights ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := &Member{Status: status, Rights: map[string]bool{}}
	for _, r := range rights {
		m.Rights[r] = true
	}
	s.members[chat][user] = m
}

// BotBlockedBy marks that a user blocked the bot (sending returns 403).
func (s *Sim) BotBlockedBy(user int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blocked[user] = true
}

func (s *Sim) BotID() int64 { return s.bot.ID }

// ---- user actions (produce updates) ----

// Send is a user writing text in a chat.
func (s *Sim) Send(chat, user int64, text string) *Msg {
	return s.sendMsg(chat, user, text, nil, 0)
}

// Reply is a user replying to a message.
func (s *Sim) Reply(chat, user int64, to *Msg, text string) *Msg {
	return s.sendMsg(chat, user, text, nil, to.ID)
}

// SendWith sends a message with extra fields (photo, contact, sticker...).
func (s *Sim) SendWith(chat, user int64, text string, extra map[string]any) *Msg {
	return s.sendMsg(chat, user, text, extra, 0)
}

func (s *Sim) sendMsg(chatID, userID int64, text string, extra map[string]any, replyTo int64) *Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	chat := s.mustChat(chatID)
	from := s.users[userID]
	if mem, ok := s.members[chatID][userID]; ok {
		muted := mem.Status == "restricted" && mem.Perms["can_send_messages"] == false &&
			(mem.Until == 0 || time.Unix(mem.Until, 0).After(time.Now()))
		if muted || mem.Status == "kicked" || mem.Status == "left" {
			s.event(chat, from.FirstName, "🚫 tried to write but can't ("+mem.Status+"): "+text, "blocked")
			return nil
		}
	}
	m := s.newMsg(chat, from, false)
	m.Text, m.Extra, m.ReplyTo = text, extra, replyTo
	if _, hasCaption := extra["caption"]; hasCaption {
		m.Caption, m.Text = text, ""
	}
	if chat.Type == "private" {
		s.started[userID] = true
	}
	s.event(chat, from.FirstName, describeUserMsg(m), "user")
	s.push("message", s.msgObject(m))
	return m
}

// ChannelPost is a post published in a channel.
func (s *Sim) ChannelPost(chatID int64, text string) *Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	chat := s.mustChat(chatID)
	m := s.newMsg(chat, nil, false)
	m.Text, m.SenderChat = text, chat
	s.event(chat, chat.Title, text, "user")
	s.push("channel_post", s.msgObject(m))
	return m
}

// Edit is a user editing their message.
func (s *Sim) Edit(m *Msg, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m.Text = text
	obj := s.msgObject(m)
	obj["edit_date"] = float64(time.Now().Unix())
	s.event(m.Chat, m.From.FirstName, "✏️ edited #"+itoa(m.ID)+": "+text, "user")
	s.push("edited_message", obj)
}

// Join adds users to a group (new_chat_members service message).
func (s *Sim) Join(chatID int64, users ...int64) *Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	chat := s.mustChat(chatID)
	var list []any
	var names []string
	for _, id := range users {
		s.members[chatID][id] = &Member{Status: "member"}
		list = append(list, s.userObject(s.users[id]))
		names = append(names, s.users[id].FirstName)
	}
	m := s.newMsg(chat, s.users[users[0]], false)
	m.Extra = map[string]any{"new_chat_members": list}
	s.event(chat, strings.Join(names, ", "), "➕ joined the group", "user")
	s.push("message", s.msgObject(m))
	return m
}

// JoinRequest is a user asking to join a chat.
func (s *Sim) JoinRequest(chatID, userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	chat := s.mustChat(chatID)
	s.joinReqs[[2]int64{chatID, userID}] = true
	s.event(chat, s.users[userID].FirstName, "🙋 requested to join", "user")
	s.push("chat_join_request", map[string]any{
		"chat": s.chatObject(chat), "from": s.userObject(s.users[userID]),
		"user_chat_id": float64(userID), "date": float64(time.Now().Unix()),
	})
}

// Press clicks the inline button with the given text on a bot message.
func (s *Sim) Press(userID int64, m *Msg, buttonText string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.Deleted {
		return fmt.Errorf("message #%d was deleted", m.ID)
	}
	var data string
	rows, _ := m.Markup["inline_keyboard"].([]any)
	for _, r := range rows {
		for _, b := range r.([]any) {
			bm := b.(map[string]any)
			if bm["text"] == buttonText {
				d, ok := bm["callback_data"].(string)
				if !ok {
					return fmt.Errorf("button %q is not a callback button", buttonText)
				}
				data = d
			}
		}
	}
	if data == "" {
		return fmt.Errorf("message #%d has no button %q (buttons: %s)", m.ID, buttonText, buttonTexts(m.Markup))
	}
	s.seq++
	id := "cbq" + itoa(int64(s.seq))
	s.callbacks[id] = &query{user: userID, chat: m.Chat.ID, msg: m.ID}
	s.event(m.Chat, s.users[userID].FirstName, "👆 pressed ["+buttonText+"]", "user")
	s.push("callback_query", map[string]any{
		"id": id, "from": s.userObject(s.users[userID]), "message": s.msgObject(m),
		"chat_instance": "ci" + itoa(m.Chat.ID), "data": data,
	})
	return nil
}

// InlineQuery is a user typing "@bot query" in any chat.
func (s *Sim) InlineQuery(userID int64, text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	id := "iq" + itoa(int64(s.seq))
	s.inline[id] = &query{user: userID}
	s.event(&Chat{ID: 0, Title: "inline mode"}, s.users[userID].FirstName, "⌨️ @"+s.bot.Username+" "+text, "user")
	s.push("inline_query", map[string]any{
		"id": id, "from": s.userObject(s.users[userID]), "query": text, "offset": "", "chat_type": "private",
	})
	return id
}

// PreCheckout is Telegram asking the bot to confirm a payment.
func (s *Sim) PreCheckout(userID int64, payload, currency string, amount int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	id := "pcq" + itoa(int64(s.seq))
	s.checkouts[id] = &query{user: userID}
	s.event(s.chats[userID], s.users[userID].FirstName, fmt.Sprintf("💳 checkout %d %s (%s)", amount, currency, payload), "user")
	s.push("pre_checkout_query", map[string]any{
		"id": id, "from": s.userObject(s.users[userID]), "currency": currency,
		"total_amount": float64(amount), "invoice_payload": payload,
	})
	return id
}

// Paid delivers the successful_payment service message.
func (s *Sim) Paid(userID int64, payload, currency string, amount int) *Msg {
	return s.SendWith(userID, userID, "", map[string]any{"successful_payment": map[string]any{
		"currency": currency, "total_amount": float64(amount), "invoice_payload": payload,
		"telegram_payment_charge_id": "tch_" + itoa(int64(amount)) + "_" + itoa(userID),
		"provider_payment_charge_id": "",
	}})
}

// Vote answers a non-anonymous poll.
func (s *Sim) Vote(userID int64, pollID string, option int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	chat := s.chats[s.polls[pollID]]
	s.event(chat, s.users[userID].FirstName, "🗳 voted option "+itoa(int64(option)), "user")
	s.push("poll_answer", map[string]any{
		"poll_id": pollID, "user": s.userObject(s.users[userID]), "option_ids": []any{float64(option)},
	})
}

// React sets a reaction; bots only receive it if they asked for
// message_reaction in allowed_updates.
func (s *Sim) React(chatID, userID int64, m *Msg, emoji string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.event(m.Chat, s.users[userID].FirstName, "reacted "+emoji+" to #"+itoa(m.ID), "user")
	s.push("message_reaction", map[string]any{
		"chat": s.chatObject(m.Chat), "message_id": float64(m.ID), "user": s.userObject(s.users[userID]),
		"date": float64(time.Now().Unix()), "old_reaction": []any{},
		"new_reaction": []any{map[string]any{"type": "emoji", "emoji": emoji}},
	})
}

// BotAddedTo adds the bot to a chat (my_chat_member update).
func (s *Sim) BotAddedTo(chatID, byUser int64, status string, rights ...string) {
	s.mu.Lock()
	chat := s.mustChat(chatID)
	s.mu.Unlock()
	s.SetMember(chatID, s.bot.ID, status, rights...)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.event(chat, s.users[byUser].FirstName, "➕ added the bot as "+status, "user")
	s.push("my_chat_member", map[string]any{
		"chat": s.chatObject(chat), "from": s.userObject(s.users[byUser]), "date": float64(time.Now().Unix()),
		"old_chat_member": map[string]any{"status": "left", "user": s.userObject(s.bot)},
		"new_chat_member": map[string]any{"status": status, "user": s.userObject(s.bot)},
	})
}

// ---- queries for assertions ----

// Handled must be wired to engine.Options.OnUpdateHandled.
func (s *Sim) Handled(map[string]any) {
	s.mu.Lock()
	s.handled++
	s.mu.Unlock()
}

// WaitIdle waits until every deliverable update has been handled.
func (s *Sim) WaitIdle(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		pending := 0
		for _, u := range s.queue {
			id := int64(u["update_id"].(float64))
			if !s.delivered[id] && s.isAllowed(u) {
				pending++
			}
		}
		done := pending == 0 && s.handled >= len(s.delivered)
		s.mu.Unlock()
		if done {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Errorf("timeout: delivered=%d handled=%d queue=%d", len(s.delivered), s.handled, len(s.queue))
}

// LastBot returns the last message the bot sent in a chat.
func (s *Sim) LastBot(chatID int64) *Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best *Msg
	for _, m := range s.msgs[chatID] {
		if m.ByBot && (best == nil || m.ID > best.ID) {
			best = m
		}
	}
	return best
}

// BotMessages returns the bot's messages in a chat, oldest first.
func (s *Sim) BotMessages(chatID int64) []*Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Msg
	for _, m := range s.msgs[chatID] {
		if m.ByBot {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Sim) Member(chat, user int64) Member {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.members[chat][user]; ok {
		return *m
	}
	return Member{Status: "left"}
}

func (s *Sim) Answered(queryID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range []map[string]*query{s.callbacks, s.inline, s.checkouts} {
		if q, ok := m[queryID]; ok {
			return q.answered
		}
	}
	return false
}

func (s *Sim) PollID(m *Msg) string {
	p, _ := m.Extra["poll"].(map[string]any)
	id, _ := p["id"].(string)
	return id
}

// CallsTo returns the bot's calls of one method.
func (s *Sim) CallsTo(method string) []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Call
	for _, c := range s.Calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// APIErrors returns the error responses Telegram gave the bot.
func (s *Sim) APIErrors() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Call
	for _, c := range s.Calls {
		if c.Error != "" {
			out = append(out, c)
		}
	}
	return out
}

// Note adds a narrative line to the transcript.
func (s *Sim) Note(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Events = append(s.Events, Event{At: time.Now(), Text: text, Kind: "note"})
}

// ---- internals ----

func (s *Sim) mustChat(id int64) *Chat {
	c, ok := s.chats[id]
	if !ok {
		panic(fmt.Sprintf("tgsim: unknown chat %d", id))
	}
	return c
}

func (s *Sim) newMsg(chat *Chat, from *User, byBot bool) *Msg {
	if s.msgs[chat.ID] == nil {
		s.msgs[chat.ID] = map[int64]*Msg{}
	}
	s.lastID[chat.ID]++
	m := &Msg{ID: s.lastID[chat.ID], Chat: chat, From: from, ByBot: byBot}
	s.msgs[chat.ID][m.ID] = m
	return m
}

func (s *Sim) event(chat *Chat, who, text, kind string) {
	label := chat.Title
	if chat.Type == "private" {
		label = "💬 " + chat.Title
	}
	s.Events = append(s.Events, Event{At: time.Now(), ChatID: chat.ID, Chat: label, Who: who, Text: text, Kind: kind})
}

func (s *Sim) push(kind string, payload map[string]any) {
	s.nextUpdate++
	u := map[string]any{"update_id": float64(s.nextUpdate), kind: payload}
	if s.allowed != nil && !s.isAllowed(u) || s.allowed == nil && defaultExcluded[kind] {
		s.Events = append(s.Events, Event{At: time.Now(), Kind: "filtered",
			Text: kind + " not delivered: the bot did not ask for it in allowed_updates"})
		return
	}
	if s.webhook != nil {
		s.delivered[int64(s.nextUpdate)] = true
		s.webhook.ch <- u
		return
	}
	s.queue = append(s.queue, u)
	close(s.wake)
	s.wake = make(chan struct{})
}

var defaultExcluded = map[string]bool{"chat_member": true, "message_reaction": true, "message_reaction_count": true}

func (s *Sim) isAllowed(u map[string]any) bool {
	for k := range u {
		if k == "update_id" {
			continue
		}
		if s.allowed == nil {
			return !defaultExcluded[k]
		}
		return s.allowed[k]
	}
	return false
}

func (s *Sim) userObject(u *User) map[string]any {
	o := map[string]any{"id": float64(u.ID), "is_bot": u.IsBot, "first_name": u.FirstName}
	if u.Username != "" {
		o["username"] = u.Username
	}
	return o
}

func (s *Sim) chatObject(c *Chat) map[string]any {
	o := map[string]any{"id": float64(c.ID), "type": c.Type}
	if c.Type == "private" {
		o["first_name"] = c.Title
	} else {
		o["title"] = c.Title
	}
	if c.Username != "" {
		o["username"] = c.Username
	}
	return o
}

func (s *Sim) msgObject(m *Msg) map[string]any {
	o := map[string]any{
		"message_id": float64(m.ID), "date": float64(time.Now().Unix()), "chat": s.chatObject(m.Chat),
	}
	if m.From != nil {
		o["from"] = s.userObject(m.From)
	}
	if m.SenderChat != nil {
		o["sender_chat"] = s.chatObject(m.SenderChat)
	}
	if m.Text != "" {
		o["text"] = m.Text
		if ents := entities(m.Text); len(ents) > 0 {
			o["entities"] = ents
		}
	}
	if m.Caption != "" {
		o["caption"] = m.Caption
		if ents := entities(m.Caption); len(ents) > 0 {
			o["caption_entities"] = ents
		}
	}
	for k, v := range m.Extra {
		if k != "caption" {
			o[k] = v
		}
	}
	if m.Markup != nil {
		if _, inline := m.Markup["inline_keyboard"]; inline {
			o["reply_markup"] = m.Markup
		}
	}
	if m.ReplyTo != 0 {
		if r, ok := s.msgs[m.Chat.ID][m.ReplyTo]; ok {
			short := map[string]any{"message_id": float64(r.ID), "date": o["date"], "chat": o["chat"]}
			if r.From != nil {
				short["from"] = s.userObject(r.From)
			}
			if r.Text != "" {
				short["text"] = r.Text
			}
			o["reply_to_message"] = short
		}
	}
	return o
}

// ---- HTTP ----

type apiError struct {
	code int
	desc string
}

func badRequest(format string, a ...any) *apiError {
	return &apiError{400, "Bad Request: " + fmt.Sprintf(format, a...)}
}

func forbidden(desc string) *apiError { return &apiError{403, "Forbidden: " + desc} }

func (s *Sim) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/bot")
	token, method, _ := strings.Cut(path, "/")
	if token != s.Token {
		writeJSON(w, map[string]any{"ok": false, "error_code": 401, "description": "Unauthorized"})
		return
	}
	params, files, multipart, err := parseRequest(r)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error_code": 400, "description": "Bad Request: " + err.Error()})
		return
	}
	present := map[string]bool{}
	for k := range files {
		present[k] = true
	}
	typed, errs := s.Spec.ValidateCall(method, params, multipart, present)
	if len(errs) > 0 {
		s.mu.Lock()
		s.Violations = append(s.Violations, errs...)
		s.Calls = append(s.Calls, Call{Method: method, Params: params, Error: "spec: " + errs[0]})
		s.mu.Unlock()
		writeJSON(w, map[string]any{"ok": false, "error_code": 400, "description": "Bad Request: " + errs[0]})
		return
	}
	for k, name := range files {
		if _, ok := typed[k]; !ok {
			typed[k] = "upload:" + name
		}
	}

	var result any
	var aerr *apiError
	if method == "getUpdates" {
		result, aerr = s.getUpdates(r.Context(), typed)
	} else {
		s.mu.Lock()
		if s.ValidateOnly {
			result = true
		} else {
			result, aerr = s.dispatch(method, typed, files)
		}
		c := Call{Method: method, Params: typed}
		if aerr != nil {
			c.Error = aerr.desc
			s.Events = append(s.Events, Event{At: time.Now(), Kind: "error", Text: method + " → " + aerr.desc, ChatID: chatIDOf(typed)})
		}
		s.Calls = append(s.Calls, c)
		s.mu.Unlock()
	}
	if aerr != nil {
		writeJSON(w, map[string]any{"ok": false, "error_code": aerr.code, "description": aerr.desc})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "result": result})
}

func chatIDOf(p map[string]any) int64 {
	f, _ := p["chat_id"].(float64)
	return int64(f)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// parseRequest accepts JSON, multipart and urlencoded bodies like Telegram.
func parseRequest(r *http.Request) (params map[string]any, files map[string]string, multipart bool, err error) {
	params, files = map[string]any{}, map[string]string{}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch ct {
	case "application/json":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, nil, false, err
		}
		if len(bytes.TrimSpace(body)) > 0 && string(body) != "null" {
			if err := json.Unmarshal(body, &params); err != nil {
				return nil, nil, false, err
			}
		}
		if params == nil {
			params = map[string]any{}
		}
	case "multipart/form-data":
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return nil, nil, false, err
		}
		for k, v := range r.MultipartForm.Value {
			params[k] = v[0]
		}
		for k, fh := range r.MultipartForm.File {
			files[k] = fh[0].Filename
		}
		multipart = true
	default:
		if err := r.ParseForm(); err != nil {
			return nil, nil, false, err
		}
		for k, v := range r.Form {
			params[k] = v[0]
		}
		multipart = true
	}
	return params, files, multipart, nil
}

func (s *Sim) getUpdates(ctx context.Context, p map[string]any) (any, *apiError) {
	s.mu.Lock()
	if s.webhook != nil {
		s.mu.Unlock()
		return nil, &apiError{409, "Conflict: can't use getUpdates method while webhook is active; use deleteWebhook to delete the webhook first"}
	}
	if list, ok := p["allowed_updates"].([]any); ok {
		s.allowed = map[string]bool{}
		for _, t := range list {
			s.allowed[t.(string)] = true
		}
		if len(list) == 0 {
			s.allowed = nil
		}
	}
	s.mu.Unlock()
	offset, _ := p["offset"].(float64)
	timeout, _ := p["timeout"].(float64)
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for {
		s.mu.Lock()
		kept := s.queue[:0]
		var out []any
		for _, u := range s.queue {
			id := u["update_id"].(float64)
			if id < offset {
				continue // confirmed by the bot
			}
			kept = append(kept, u)
			if s.isAllowed(u) {
				out = append(out, u)
				s.delivered[int64(id)] = true
			}
		}
		s.queue = kept
		wake := s.wake
		s.mu.Unlock()
		if len(out) > 0 || time.Now().After(deadline) {
			if out == nil {
				out = []any{}
			}
			return out, nil
		}
		select {
		case <-wake:
		case <-time.After(time.Until(deadline)):
		case <-ctx.Done():
			return []any{}, nil
		}
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
