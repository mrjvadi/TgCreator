package tgsim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf16"
)

var mediaMethods = map[string]string{
	"sendPhoto": "photo", "sendVideo": "video", "sendAudio": "audio", "sendDocument": "document",
	"sendAnimation": "animation", "sendVoice": "voice", "sendVideoNote": "video_note", "sendSticker": "sticker",
}

var mediaIcons = map[string]string{
	"photo": "📷", "video": "🎬", "audio": "🎵", "document": "📄", "animation": "🎞",
	"voice": "🎤", "video_note": "⏺", "sticker": "🏷",
}

// dispatch simulates a method. Called with s.mu held.
func (s *Sim) dispatch(method string, p map[string]any, files map[string]string) (any, *apiError) {
	if kind, ok := mediaMethods[method]; ok {
		return s.sendMedia(kind, p)
	}
	switch method {
	case "getMe":
		o := s.userObject(s.bot)
		o["can_join_groups"], o["can_read_all_group_messages"], o["supports_inline_queries"] = true, false, true
		return o, nil
	case "deleteWebhook":
		if s.webhook != nil {
			close(s.webhook.ch)
			s.webhook = nil
		}
		if b, _ := p["drop_pending_updates"].(bool); b {
			s.queue = nil
		}
		return true, nil
	case "setWebhook":
		return s.setWebhook(p)
	case "getWebhookInfo":
		info := map[string]any{"url": "", "has_custom_certificate": false, "pending_update_count": float64(len(s.queue))}
		if s.webhook != nil {
			info["url"] = s.webhook.url
		}
		return info, nil
	case "sendMessage":
		return s.sendText(p)
	case "sendMediaGroup":
		return s.sendMediaGroup(p)
	case "sendPoll":
		return s.sendPoll(p)
	case "sendDice":
		return s.sendSimple(p, "dice", func(m *Msg) {
			emoji, _ := p["emoji"].(string)
			if emoji == "" {
				emoji = "🎲"
			}
			m.Extra = map[string]any{"dice": map[string]any{"emoji": emoji, "value": float64(4)}}
		}, "🎲 dice → 4")
	case "sendLocation":
		return s.sendSimple(p, "location", func(m *Msg) {
			m.Extra = map[string]any{"location": map[string]any{"latitude": p["latitude"], "longitude": p["longitude"]}}
		}, fmt.Sprintf("📍 location %v,%v", p["latitude"], p["longitude"]))
	case "sendContact":
		return s.sendSimple(p, "contact", func(m *Msg) {
			m.Extra = map[string]any{"contact": map[string]any{"phone_number": p["phone_number"], "first_name": p["first_name"]}}
		}, fmt.Sprintf("👤 contact %v", p["phone_number"]))
	case "sendInvoice":
		return s.sendInvoice(p)
	case "editMessageText", "editMessageCaption", "editMessageReplyMarkup":
		return s.editMessage(method, p)
	case "deleteMessage":
		return s.deleteMessage(p, true)
	case "deleteMessages":
		ids, _ := p["message_ids"].([]any)
		for _, id := range ids {
			q := map[string]any{"chat_id": p["chat_id"], "message_id": id}
			_, _ = s.deleteMessage(q, false)
		}
		return true, nil
	case "forwardMessage", "copyMessage":
		return s.copyMessage(method, p)
	case "answerCallbackQuery":
		return s.answerCallback(p)
	case "answerInlineQuery":
		return s.answerInline(p)
	case "answerPreCheckoutQuery":
		return s.answerPreCheckout(p)
	case "getChat":
		chat, err := s.chatOf(p["chat_id"])
		if err != nil {
			return nil, err
		}
		o := s.chatObject(chat)
		o["accent_color_id"], o["max_reaction_count"] = float64(0), float64(11)
		return o, nil
	case "getChatMember":
		return s.getChatMember(p)
	case "getChatAdministrators":
		chat, err := s.chatOf(p["chat_id"])
		if err != nil {
			return nil, err
		}
		var out []any
		for uid, m := range s.members[chat.ID] {
			if m.Status == "creator" || m.Status == "administrator" {
				out = append(out, s.memberObject(uid, m))
			}
		}
		return out, nil
	case "getChatMemberCount":
		chat, err := s.chatOf(p["chat_id"])
		if err != nil {
			return nil, err
		}
		n := 0
		for _, m := range s.members[chat.ID] {
			if m.Status != "left" && m.Status != "kicked" {
				n++
			}
		}
		return float64(n), nil
	case "banChatMember", "unbanChatMember", "restrictChatMember", "promoteChatMember":
		return s.moderate(method, p)
	case "pinChatMessage", "unpinChatMessage":
		return s.pin(method, p)
	case "approveChatJoinRequest", "declineChatJoinRequest":
		return s.joinRequest(method, p)
	case "setMessageReaction":
		m, err := s.findMsg(p, "message to react not found")
		if err != nil {
			return nil, err
		}
		var emojis []string
		list, _ := p["reaction"].([]any)
		for _, r := range list {
			emojis = append(emojis, fmt.Sprint(r.(map[string]any)["emoji"]))
		}
		m.Reactions = emojis
		s.event(m.Chat, "🤖 bot", "reacted "+strings.Join(emojis, "")+" to #"+itoa(m.ID), "action")
		return true, nil
	case "leaveChat":
		chat, err := s.chatOf(p["chat_id"])
		if err != nil {
			return nil, err
		}
		s.members[chat.ID][s.bot.ID] = &Member{Status: "left"}
		s.event(chat, "🤖 bot", "left the chat", "action")
		return true, nil
	case "sendChatAction":
		if _, err := s.chatOf(p["chat_id"]); err != nil {
			return nil, err
		}
		return true, nil
	}
	// Valid per spec but not modelled: accept like Telegram would.
	return true, nil
}

func (s *Sim) chatOf(v any) (*Chat, *apiError) {
	switch t := v.(type) {
	case float64:
		if c, ok := s.chats[int64(t)]; ok {
			return c, nil
		}
	case string:
		for _, c := range s.chats {
			if c.Username != "" && "@"+c.Username == t {
				return c, nil
			}
		}
	}
	return nil, badRequest("chat not found")
}

func (s *Sim) botMember(chat *Chat) *Member {
	if m, ok := s.members[chat.ID][s.bot.ID]; ok {
		return m
	}
	return &Member{Status: "left"}
}

func (s *Sim) botCan(chat *Chat, right string) bool {
	m := s.botMember(chat)
	return m.Status == "administrator" && m.Rights[right]
}

// canSend applies Telegram's delivery rules.
func (s *Sim) canSend(chat *Chat) *apiError {
	switch chat.Type {
	case "private":
		if s.blocked[chat.ID] {
			return forbidden("bot was blocked by the user")
		}
		if !s.started[chat.ID] && !s.pendingJoinRequest(chat.ID) {
			return forbidden("bot can't initiate conversation with a user")
		}
	case "channel":
		if !s.botCan(chat, "can_post_messages") {
			return badRequest("need administrator rights in the channel chat")
		}
	default:
		switch s.botMember(chat).Status {
		case "left":
			return forbidden("bot is not a member of the " + chat.Type + " chat")
		case "kicked":
			return forbidden("bot was kicked from the " + chat.Type + " chat")
		}
	}
	return nil
}

// prepareSend handles what every send* method shares.
func (s *Sim) prepareSend(p map[string]any) (*Chat, *apiError) {
	chat, err := s.chatOf(p["chat_id"])
	if err != nil {
		return nil, err
	}
	if err := s.canSend(chat); err != nil {
		return nil, err
	}
	if rp, ok := p["reply_parameters"].(map[string]any); ok {
		id, _ := rp["message_id"].(float64)
		allow, _ := rp["allow_sending_without_reply"].(bool)
		if m, ok := s.msgs[chat.ID][int64(id)]; (!ok || m.Deleted) && !allow {
			return nil, badRequest("message to be replied not found")
		}
	}
	return chat, nil
}

func (s *Sim) finishSend(chat *Chat, m *Msg, p map[string]any, summary string) any {
	if rp, ok := p["reply_parameters"].(map[string]any); ok {
		id, _ := rp["message_id"].(float64)
		if r, ok := s.msgs[chat.ID][int64(id)]; ok && !r.Deleted {
			m.ReplyTo = r.ID
		}
	}
	if rm, ok := p["reply_markup"].(map[string]any); ok {
		m.Markup = rm
		summary += markupSummary(rm)
	}
	s.event(chat, "🤖 bot", "#"+itoa(m.ID)+" "+summary, "bot")
	return s.msgObject(m)
}

func (s *Sim) formatted(p map[string]any, key string, limit int) (string, *apiError) {
	text, _ := p[key].(string)
	if pm, _ := p["parse_mode"].(string); strings.EqualFold(pm, "HTML") {
		plain, err := checkHTML(text)
		if err != nil {
			return "", badRequest("can't parse entities: %s", err.Error())
		}
		text = plain
	}
	if n := len(utf16.Encode([]rune(text))); n > limit {
		if key == "text" {
			return "", badRequest("message is too long")
		}
		return "", badRequest("message caption is too long")
	}
	return text, nil
}

func (s *Sim) sendText(p map[string]any) (any, *apiError) {
	chat, err := s.prepareSend(p)
	if err != nil {
		return nil, err
	}
	text, err := s.formatted(p, "text", 4096)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, badRequest("message text is empty")
	}
	m := s.newMsg(chat, s.bot, true)
	m.Text = text
	return s.finishSend(chat, m, p, text), nil
}

func fileDesc(v any) string {
	s, _ := v.(string)
	switch {
	case strings.HasPrefix(s, "upload:"):
		return "uploaded " + strings.TrimPrefix(s, "upload:")
	case strings.HasPrefix(s, "attach://"):
		return "uploaded (" + s + ")"
	case strings.HasPrefix(s, "http"):
		return "from URL " + s
	default:
		return "file_id " + s
	}
}

var fileSeq atomic.Int64

func fileObject(kind string) any {
	seq := fileSeq.Add(1)
	id := fmt.Sprintf("SIM_%s_%d", strings.ToUpper(kind), seq)
	base := map[string]any{"file_id": id, "file_unique_id": "u" + id, "file_size": float64(1024 * seq)}
	switch kind {
	case "photo":
		base["width"], base["height"] = float64(800), float64(600)
		return []any{base}
	case "video", "animation":
		base["width"], base["height"], base["duration"] = float64(640), float64(360), float64(5)
	case "audio", "voice":
		base["duration"] = float64(30)
	case "video_note":
		base["length"], base["duration"] = float64(240), float64(10)
	case "sticker":
		base["type"], base["width"], base["height"] = "regular", float64(512), float64(512)
		base["is_animated"], base["is_video"] = false, false
	}
	return base
}

func (s *Sim) sendMedia(kind string, p map[string]any) (any, *apiError) {
	chat, err := s.prepareSend(p)
	if err != nil {
		return nil, err
	}
	caption, err := s.formatted(p, "caption", 1024)
	if err != nil {
		return nil, err
	}
	m := s.newMsg(chat, s.bot, true)
	m.Media, m.Caption = kind, caption
	m.Extra = map[string]any{kind: fileObject(kind)}
	summary := mediaIcons[kind] + " " + kind + " (" + fileDesc(p[kind]) + ")"
	if caption != "" {
		summary += " — " + caption
	}
	return s.finishSend(chat, m, p, summary), nil
}

func (s *Sim) sendMediaGroup(p map[string]any) (any, *apiError) {
	chat, err := s.prepareSend(p)
	if err != nil {
		return nil, err
	}
	media, _ := p["media"].([]any)
	if len(media) < 2 || len(media) > 10 {
		return nil, badRequest("wrong number of messages in the media group: must be 2-10")
	}
	var out []any
	for _, it := range media {
		im := it.(map[string]any)
		kind := im["type"].(string)
		m := s.newMsg(chat, s.bot, true)
		m.Media = kind
		m.Extra = map[string]any{kind: fileObject(kind), "media_group_id": "album1"}
		if c, ok := im["caption"].(string); ok {
			m.Caption = c
		}
		s.event(chat, "🤖 bot", "#"+itoa(m.ID)+" "+mediaIcons[kind]+" album "+kind+" ("+fileDesc(im["media"])+")", "bot")
		out = append(out, s.msgObject(m))
	}
	return out, nil
}

func (s *Sim) sendSimple(p map[string]any, kind string, fill func(*Msg), summary string) (any, *apiError) {
	chat, err := s.prepareSend(p)
	if err != nil {
		return nil, err
	}
	m := s.newMsg(chat, s.bot, true)
	m.Media = kind
	fill(m)
	return s.finishSend(chat, m, p, summary), nil
}

func (s *Sim) sendPoll(p map[string]any) (any, *apiError) {
	opts, _ := p["options"].([]any)
	if len(opts) < 2 {
		return nil, badRequest("poll must have at least 2 option")
	}
	if len(opts) > 12 {
		return nil, badRequest("poll can't have more than 12 options")
	}
	chat, err := s.prepareSend(p)
	if err != nil {
		return nil, err
	}
	s.seq++
	id := "poll" + itoa(int64(s.seq))
	s.polls[id] = chat.ID
	var options []any
	var names []string
	for _, o := range opts {
		t := o.(map[string]any)["text"]
		options = append(options, map[string]any{"text": t, "voter_count": float64(0)})
		names = append(names, fmt.Sprint(t))
	}
	anon := true
	if a, ok := p["is_anonymous"].(bool); ok {
		anon = a
	}
	m := s.newMsg(chat, s.bot, true)
	m.Media = "poll"
	m.Extra = map[string]any{"poll": map[string]any{
		"id": id, "question": p["question"], "options": options, "total_voter_count": float64(0),
		"is_closed": false, "is_anonymous": anon, "type": "regular", "allows_multiple_answers": false,
	}}
	return s.finishSend(chat, m, p, fmt.Sprintf("📊 poll «%v» [%s]", p["question"], strings.Join(names, " | "))), nil
}

func (s *Sim) sendInvoice(p map[string]any) (any, *apiError) {
	currency, _ := p["currency"].(string)
	prices, _ := p["prices"].([]any)
	if currency == "XTR" {
		if len(prices) != 1 {
			return nil, badRequest("STARS_INVOICE_INVALID")
		}
		if tok, _ := p["provider_token"].(string); tok != "" {
			return nil, badRequest("PROVIDER_TOKEN_INVALID")
		}
	}
	chat, err := s.prepareSend(p)
	if err != nil {
		return nil, err
	}
	total := 0.0
	for _, pr := range prices {
		total += pr.(map[string]any)["amount"].(float64)
	}
	m := s.newMsg(chat, s.bot, true)
	m.Media = "invoice"
	m.Extra = map[string]any{"invoice": map[string]any{
		"title": p["title"], "description": p["description"], "start_parameter": "",
		"currency": currency, "total_amount": total,
	}}
	return s.finishSend(chat, m, p, fmt.Sprintf("🧾 invoice «%v» %v %s", p["title"], total, currency)), nil
}

func (s *Sim) findMsg(p map[string]any, notFound string) (*Msg, *apiError) {
	chat, err := s.chatOf(p["chat_id"])
	if err != nil {
		return nil, err
	}
	id, _ := p["message_id"].(float64)
	m, ok := s.msgs[chat.ID][int64(id)]
	if !ok || m.Deleted {
		return nil, badRequest("%s", notFound)
	}
	return m, nil
}

func (s *Sim) editMessage(method string, p map[string]any) (any, *apiError) {
	if _, inline := p["inline_message_id"]; inline {
		return true, nil
	}
	m, err := s.findMsg(p, "message to edit not found")
	if err != nil {
		return nil, err
	}
	if !m.ByBot && !(m.Chat.Type == "channel" && s.botCan(m.Chat, "can_edit_messages")) {
		return nil, badRequest("message can't be edited")
	}
	newMarkup, _ := p["reply_markup"].(map[string]any)
	sameMarkup := jsonEqual(newMarkup, m.Markup)
	var summary string
	switch method {
	case "editMessageText":
		if m.Media != "" {
			return nil, badRequest("there is no text in the message to edit")
		}
		text, err := s.formatted(p, "text", 4096)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(text) == "" {
			return nil, badRequest("message text is empty")
		}
		if text == m.Text && sameMarkup {
			return nil, badRequest("message is not modified: specified new message content and reply markup are exactly the same as a current content and reply markup of the message")
		}
		m.Text = text
		summary = "✏️ edited #" + itoa(m.ID) + " → " + text
	case "editMessageCaption":
		if m.Media == "" {
			return nil, badRequest("there is no caption in the message to edit")
		}
		caption, err := s.formatted(p, "caption", 1024)
		if err != nil {
			return nil, err
		}
		if caption == m.Caption && sameMarkup {
			return nil, badRequest("message is not modified: specified new message content and reply markup are exactly the same as a current content and reply markup of the message")
		}
		m.Caption = caption
		summary = "✏️ edited caption #" + itoa(m.ID) + " → " + caption
	default:
		if sameMarkup {
			return nil, badRequest("message is not modified: specified new message content and reply markup are exactly the same as a current content and reply markup of the message")
		}
		summary = "✏️ edited buttons of #" + itoa(m.ID)
	}
	m.Markup = newMarkup
	summary += markupSummary(newMarkup)
	s.event(m.Chat, "🤖 bot", summary, "action")
	return s.msgObject(m), nil
}

func (s *Sim) deleteMessage(p map[string]any, log bool) (any, *apiError) {
	m, err := s.findMsg(p, "message to delete not found")
	if err != nil {
		return nil, err
	}
	switch m.Chat.Type {
	case "private":
	default:
		if !m.ByBot && !s.botCan(m.Chat, "can_delete_messages") {
			return nil, badRequest("message can't be deleted")
		}
	}
	m.Deleted = true
	who := "?"
	if m.From != nil {
		who = m.From.FirstName
	}
	if m.ByBot {
		who = "bot"
	}
	s.event(m.Chat, "🤖 bot", "🗑 deleted #"+itoa(m.ID)+" ("+who+")", "action")
	return true, nil
}

func (s *Sim) copyMessage(method string, p map[string]any) (any, *apiError) {
	from, err := s.chatOf(p["from_chat_id"])
	if err != nil {
		return nil, err
	}
	id, _ := p["message_id"].(float64)
	src, ok := s.msgs[from.ID][int64(id)]
	if !ok || src.Deleted {
		return nil, badRequest("message to copy not found")
	}
	chat, aerr := s.prepareSend(p)
	if aerr != nil {
		return nil, aerr
	}
	m := s.newMsg(chat, s.bot, true)
	m.Text, m.Caption, m.Media, m.Extra = src.Text, src.Caption, src.Media, src.Extra
	obj := s.finishSend(chat, m, p, "↪️ "+method+" of #"+itoa(src.ID)+" from "+from.Title+": "+src.Text+src.Caption)
	if method == "copyMessage" {
		return map[string]any{"message_id": float64(m.ID)}, nil
	}
	return obj, nil
}

func (s *Sim) answerCallback(p map[string]any) (any, *apiError) {
	id, _ := p["callback_query_id"].(string)
	q, ok := s.callbacks[id]
	if !ok || q.answered {
		return nil, badRequest("query is too old and response timeout expired or query ID is invalid")
	}
	q.answered = true
	if text, _ := p["text"].(string); text != "" {
		kind := "toast"
		if alert, _ := p["show_alert"].(bool); alert {
			kind = "alert"
		}
		s.event(s.chats[q.chat], "🤖 bot", "💬 "+kind+" to "+s.users[q.user].FirstName+": "+text, "bot")
	}
	return true, nil
}

func (s *Sim) answerInline(p map[string]any) (any, *apiError) {
	id, _ := p["inline_query_id"].(string)
	q, ok := s.inline[id]
	if !ok || q.answered {
		return nil, badRequest("query is too old and response timeout expired or query ID is invalid")
	}
	results, _ := p["results"].([]any)
	if len(results) > 50 {
		return nil, badRequest("RESULTS_TOO_MUCH")
	}
	seen := map[string]bool{}
	var titles []string
	for _, r := range results {
		rm := r.(map[string]any)
		rid := fmt.Sprint(rm["id"])
		if seen[rid] {
			return nil, badRequest("RESULT_ID_DUPLICATE")
		}
		seen[rid] = true
		titles = append(titles, fmt.Sprint(rm["title"]))
	}
	q.answered = true
	s.event(&Chat{Title: "inline mode"}, "🤖 bot", "inline results: "+strings.Join(titles, " | "), "bot")
	return true, nil
}

func (s *Sim) answerPreCheckout(p map[string]any) (any, *apiError) {
	id, _ := p["pre_checkout_query_id"].(string)
	q, ok := s.checkouts[id]
	if !ok || q.answered {
		return nil, badRequest("QUERY_ID_INVALID")
	}
	okv, _ := p["ok"].(bool)
	if msg, _ := p["error_message"].(string); !okv && msg == "" {
		return nil, badRequest("ERROR_MESSAGE_EMPTY")
	}
	q.answered = true
	verdict := "✅ payment approved"
	if !okv {
		verdict = fmt.Sprintf("❌ payment rejected: %v", p["error_message"])
	}
	s.event(s.chats[q.user], "🤖 bot", verdict, "bot")
	return true, nil
}

func (s *Sim) memberObject(uid int64, m *Member) map[string]any {
	o := map[string]any{"status": m.Status, "user": s.userObject(s.users[uid])}
	switch m.Status {
	case "creator":
		o["is_anonymous"] = false
	case "administrator":
		o["can_be_edited"], o["is_anonymous"] = false, false
		for r, v := range m.Rights {
			o[r] = v
		}
	case "restricted":
		o["is_member"], o["until_date"] = true, float64(m.Until)
		for k, v := range m.Perms {
			o[k] = v
		}
	case "kicked":
		o["until_date"] = float64(m.Until)
	}
	return o
}

func (s *Sim) getChatMember(p map[string]any) (any, *apiError) {
	chat, err := s.chatOf(p["chat_id"])
	if err != nil {
		return nil, err
	}
	uid, _ := p["user_id"].(float64)
	if _, ok := s.users[int64(uid)]; !ok {
		return nil, badRequest("user not found")
	}
	if chat.Type == "private" {
		return s.memberObject(int64(uid), &Member{Status: "member"}), nil
	}
	m, ok := s.members[chat.ID][int64(uid)]
	if !ok {
		m = &Member{Status: "left"}
	}
	return s.memberObject(int64(uid), m), nil
}

func (s *Sim) moderate(method string, p map[string]any) (any, *apiError) {
	chat, err := s.chatOf(p["chat_id"])
	if err != nil {
		return nil, err
	}
	uid64, _ := p["user_id"].(float64)
	uid := int64(uid64)
	user, ok := s.users[uid]
	if !ok {
		return nil, badRequest("user not found")
	}
	if chat.Type == "private" {
		return nil, badRequest("method is available for supergroup and channel chats only")
	}
	if method == "restrictChatMember" && chat.Type != "supergroup" {
		return nil, badRequest("method is available only for supergroups")
	}
	right := "can_restrict_members"
	if method == "promoteChatMember" {
		right = "can_promote_members"
	}
	if !s.botCan(chat, right) {
		return nil, badRequest("not enough rights to restrict/unrestrict chat member")
	}
	cur, ok := s.members[chat.ID][uid]
	if !ok {
		cur = &Member{Status: "left"}
		s.members[chat.ID][uid] = cur
	}
	if cur.Status == "creator" {
		return nil, badRequest("can't remove chat owner")
	}
	if cur.Status == "administrator" && method != "promoteChatMember" && method != "unbanChatMember" {
		return nil, badRequest("user is an administrator of the chat")
	}
	until, _ := p["until_date"].(float64)
	var what string
	switch method {
	case "banChatMember":
		cur.Status, cur.Until = "kicked", int64(until)
		what = "⛔️ banned " + user.FirstName
	case "unbanChatMember":
		if only, _ := p["only_if_banned"].(bool); only && cur.Status != "kicked" {
			return true, nil
		}
		if cur.Status == "kicked" {
			cur.Status = "left"
		}
		what = "unbanned " + user.FirstName
	case "restrictChatMember":
		perms, _ := p["permissions"].(map[string]any)
		all := len(perms) > 0
		for _, v := range perms {
			if v != true {
				all = false
			}
		}
		if all {
			cur.Status, cur.Perms, cur.Until = "member", nil, 0
			what = "🔓 lifted restrictions of " + user.FirstName
		} else {
			cur.Status, cur.Perms, cur.Until = "restricted", perms, int64(until)
			what = "🔇 restricted " + user.FirstName
			if until > 0 {
				what += fmt.Sprintf(" for %s", time.Until(time.Unix(int64(until), 0)).Round(time.Minute))
			}
		}
	case "promoteChatMember":
		cur.Status = "administrator"
		what = "promoted " + user.FirstName
	}
	s.event(chat, "🤖 bot", what, "action")
	return true, nil
}

func (s *Sim) pin(method string, p map[string]any) (any, *apiError) {
	chat, err := s.chatOf(p["chat_id"])
	if err != nil {
		return nil, err
	}
	if chat.Type != "private" && !s.botCan(chat, "can_pin_messages") && !(chat.Type == "channel" && s.botCan(chat, "can_edit_messages")) {
		return nil, badRequest("not enough rights to manage pinned messages in the chat")
	}
	if method == "unpinChatMessage" {
		for _, m := range s.msgs[chat.ID] {
			m.Pinned = false
		}
		s.event(chat, "🤖 bot", "unpinned", "action")
		return true, nil
	}
	m, aerr := s.findMsg(p, "message to pin not found")
	if aerr != nil {
		return nil, aerr
	}
	m.Pinned = true
	s.event(chat, "🤖 bot", "📌 pinned #"+itoa(m.ID), "action")
	return true, nil
}

func (s *Sim) joinRequest(method string, p map[string]any) (any, *apiError) {
	chat, err := s.chatOf(p["chat_id"])
	if err != nil {
		return nil, err
	}
	uid, _ := p["user_id"].(float64)
	key := [2]int64{chat.ID, int64(uid)}
	if !s.joinReqs[key] {
		return nil, badRequest("HIDE_REQUESTER_MISSING")
	}
	if !s.botCan(chat, "can_invite_users") {
		return nil, badRequest("not enough rights")
	}
	delete(s.joinReqs, key)
	if method == "approveChatJoinRequest" {
		s.members[chat.ID][int64(uid)] = &Member{Status: "member"}
		s.event(chat, "🤖 bot", "✅ approved join request of "+s.users[int64(uid)].FirstName, "action")
	} else {
		s.event(chat, "🤖 bot", "❌ declined join request of "+s.users[int64(uid)].FirstName, "action")
	}
	return true, nil
}

func (s *Sim) setWebhook(p map[string]any) (any, *apiError) {
	url, _ := p["url"].(string)
	if url == "" {
		return true, nil
	}
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://127.0.0.1") {
		return nil, badRequest("bad webhook: An HTTPS URL must be provided for webhook")
	}
	if list, ok := p["allowed_updates"].([]any); ok && len(list) > 0 {
		s.allowed = map[string]bool{}
		for _, t := range list {
			s.allowed[t.(string)] = true
		}
	}
	secret, _ := p["secret_token"].(string)
	wh := &webhookCfg{url: url, secret: secret, ch: make(chan map[string]any, 1024)}
	s.webhook = wh
	go s.deliverWebhook(wh)
	return true, nil
}

func (s *Sim) deliverWebhook(wh *webhookCfg) {
	for u := range wh.ch {
		body, _ := json.Marshal(u)
		for attempt := 0; attempt < 50; attempt++ {
			req, _ := http.NewRequest(http.MethodPost, wh.url, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if wh.secret != "" {
				req.Header.Set("X-Telegram-Bot-Api-Secret-Token", wh.secret)
			}
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// ---- helpers ----

var urlRe = regexp.MustCompile(`(?i)\b((?:https?://|www\.|t\.me/)[^\s]+|[a-z0-9-]+\.(?:com|org|net|ir|io)\b[^\s]*)`)

// entities marks commands and links the way Telegram clients do.
func entities(text string) []any {
	var out []any
	u16 := func(s string) float64 { return float64(len(utf16.Encode([]rune(s)))) }
	if strings.HasPrefix(text, "/") {
		cmd, _, _ := strings.Cut(text, " ")
		out = append(out, map[string]any{"type": "bot_command", "offset": float64(0), "length": u16(cmd)})
	}
	for _, loc := range urlRe.FindAllStringIndex(text, -1) {
		out = append(out, map[string]any{"type": "url", "offset": u16(text[:loc[0]]), "length": u16(text[loc[0]:loc[1]])})
	}
	return out
}

var allowedTags = map[string]bool{
	"b": true, "strong": true, "i": true, "em": true, "u": true, "ins": true, "s": true, "strike": true,
	"del": true, "span": true, "tg-spoiler": true, "a": true, "code": true, "pre": true,
	"blockquote": true, "tg-emoji": true, "tg-time": true,
}

// checkHTML validates Telegram's HTML subset and returns the plain text.
func checkHTML(s string) (string, error) {
	var stack []string
	var plain strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '<' {
			j := strings.IndexByte(s[i:], '<')
			if j < 0 {
				j = len(s) - i
			}
			plain.WriteString(s[i : i+j])
			i += j
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			return "", fmt.Errorf("unclosed start tag at byte offset %d", i)
		}
		tag := s[i+1 : i+end]
		closing := strings.HasPrefix(tag, "/")
		name := strings.ToLower(strings.Fields(strings.TrimPrefix(tag, "/") + " ")[0])
		if !allowedTags[name] {
			return "", fmt.Errorf("Unsupported start tag %q at byte offset %d", name, i)
		}
		if closing {
			if len(stack) == 0 || stack[len(stack)-1] != name {
				return "", fmt.Errorf("Unmatched end tag at byte offset %d, expected \"</%s>\", found \"</%s>\"", i, top(stack), name)
			}
			stack = stack[:len(stack)-1]
		} else {
			stack = append(stack, name)
		}
		i += end + 1
	}
	if len(stack) > 0 {
		return "", fmt.Errorf("Can't find end tag corresponding to start tag %q", stack[len(stack)-1])
	}
	return html.UnescapeString(plain.String()), nil
}

func top(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1]
}

func markupSummary(rm map[string]any) string {
	if rm == nil {
		return ""
	}
	if _, ok := rm["inline_keyboard"]; ok {
		return "  " + buttonTexts(rm)
	}
	if rows, ok := rm["keyboard"].([]any); ok {
		var names []string
		for _, r := range rows {
			for _, b := range r.([]any) {
				if bm, ok := b.(map[string]any); ok {
					names = append(names, fmt.Sprint(bm["text"]))
				} else {
					names = append(names, fmt.Sprint(b))
				}
			}
		}
		return "  ⌨️ keyboard: " + strings.Join(names, " | ")
	}
	if rm["remove_keyboard"] == true {
		return "  (keyboard removed)"
	}
	return ""
}

func buttonTexts(rm map[string]any) string {
	rows, _ := rm["inline_keyboard"].([]any)
	var names []string
	for _, r := range rows {
		for _, b := range r.([]any) {
			names = append(names, "["+fmt.Sprint(b.(map[string]any)["text"])+"]")
		}
	}
	return strings.Join(names, " ")
}

func describeUserMsg(m *Msg) string {
	var parts []string
	for k, v := range m.Extra {
		switch k {
		case "photo", "video", "document", "sticker", "voice", "audio", "animation", "video_note":
			parts = append(parts, mediaIcons[k]+" "+k)
		case "contact":
			parts = append(parts, fmt.Sprintf("👤 shared contact %v", v.(map[string]any)["phone_number"]))
		case "successful_payment":
			sp := v.(map[string]any)
			parts = append(parts, fmt.Sprintf("✅ paid %v %v", sp["total_amount"], sp["currency"]))
		}
	}
	text := m.Text + m.Caption
	if m.ReplyTo != 0 {
		text = "↩️ reply to #" + itoa(m.ReplyTo) + ": " + text
	}
	if text != "" {
		parts = append(parts, text)
	}
	return "#" + itoa(m.ID) + " " + strings.Join(parts, " ")
}

func jsonEqual(a, b map[string]any) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// A pending join request lets the bot message the user (user_chat_id).
func (s *Sim) pendingJoinRequest(user int64) bool {
	for k := range s.joinReqs {
		if k[1] == user {
			return true
		}
	}
	return false
}
