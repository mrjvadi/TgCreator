package tgsim

import (
	"fmt"
	"sort"
)

// Views are JSON-friendly snapshots used by the web panel's test chat.

type ButtonView struct {
	Text string `json:"text"`
	Data string `json:"data,omitempty"`
	URL  string `json:"url,omitempty"`
}

type KeyButtonView struct {
	Text           string `json:"text"`
	RequestContact bool   `json:"request_contact,omitempty"`
}

type MsgView struct {
	ID        int64          `json:"id"`
	ChatID    int64          `json:"chat_id"`
	From      string         `json:"from"`
	FromID    int64          `json:"from_id,omitempty"`
	ByBot     bool           `json:"by_bot"`
	Text      string         `json:"text,omitempty"`
	Caption   string         `json:"caption,omitempty"`
	Media     string         `json:"media,omitempty"`
	Info      string         `json:"info,omitempty"`
	Buttons   [][]ButtonView `json:"buttons,omitempty"`
	Deleted   bool           `json:"deleted,omitempty"`
	Pinned    bool           `json:"pinned,omitempty"`
	Reactions []string       `json:"reactions,omitempty"`
	ReplyTo   int64          `json:"reply_to,omitempty"`
}

type ChatView struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

type MemberView struct {
	UserID int64  `json:"user_id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// ChatSnapshot is everything needed to draw one chat.
type ChatSnapshot struct {
	Chat     ChatView          `json:"chat"`
	Messages []MsgView         `json:"messages"`
	Keyboard [][]KeyButtonView `json:"keyboard,omitempty"`
	Members  []MemberView      `json:"members,omitempty"`
}

func (s *Sim) msgView(m *Msg) MsgView {
	v := MsgView{ID: m.ID, ChatID: m.Chat.ID, ByBot: m.ByBot, Text: m.Text, Caption: m.Caption, Media: m.Media,
		Deleted: m.Deleted, Pinned: m.Pinned, Reactions: m.Reactions, ReplyTo: m.ReplyTo}
	switch {
	case m.ByBot:
		v.From = s.bot.FirstName
	case m.From != nil:
		v.From, v.FromID = m.From.FirstName, m.From.ID
	case m.SenderChat != nil:
		v.From = m.SenderChat.Title
	}
	if rows, ok := m.Markup["inline_keyboard"].([]any); ok {
		for _, r := range rows {
			var row []ButtonView
			for _, b := range r.([]any) {
				bm, _ := b.(map[string]any)
				bv := ButtonView{Text: fmt.Sprint(bm["text"])}
				if d, ok := bm["callback_data"].(string); ok {
					bv.Data = d
				}
				if u, ok := bm["url"].(string); ok {
					bv.URL = u
				}
				row = append(row, bv)
			}
			v.Buttons = append(v.Buttons, row)
		}
	}
	for k, x := range m.Extra {
		xm, _ := x.(map[string]any)
		switch k {
		case "poll":
			v.Info = fmt.Sprintf("📊 %v", xm["question"])
		case "dice":
			v.Info = fmt.Sprintf("%v → %v", xm["emoji"], xm["value"])
		case "invoice":
			v.Info = fmt.Sprintf("🧾 %v — %v %v", xm["title"], xm["total_amount"], xm["currency"])
		case "contact":
			v.Info = fmt.Sprintf("👤 %v", xm["phone_number"])
		case "location":
			v.Info = fmt.Sprintf("📍 %v, %v", xm["latitude"], xm["longitude"])
		case "new_chat_members":
			v.Info = "➕ عضو شد"
		case "successful_payment":
			v.Info = fmt.Sprintf("✅ پرداخت %v %v", xm["total_amount"], xm["currency"])
		}
	}
	return v
}

// Snapshot returns a chat's messages, active reply keyboard and members.
func (s *Sim) Snapshot(chatID int64) ChatSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.chats[chatID]
	if !ok {
		return ChatSnapshot{}
	}
	snap := ChatSnapshot{Chat: ChatView{ID: c.ID, Type: c.Type, Title: c.Title}, Messages: []MsgView{}}
	var ids []int64
	for id := range s.msgs[chatID] {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		m := s.msgs[chatID][id]
		snap.Messages = append(snap.Messages, s.msgView(m))
		if !m.ByBot || m.Markup == nil {
			continue
		}
		if rows, ok := m.Markup["keyboard"].([]any); ok {
			snap.Keyboard = nil
			for _, r := range rows {
				var row []KeyButtonView
				for _, b := range r.([]any) {
					switch t := b.(type) {
					case string:
						row = append(row, KeyButtonView{Text: t})
					case map[string]any:
						rc, _ := t["request_contact"].(bool)
						row = append(row, KeyButtonView{Text: fmt.Sprint(t["text"]), RequestContact: rc})
					}
				}
				snap.Keyboard = append(snap.Keyboard, row)
			}
		} else if m.Markup["remove_keyboard"] == true {
			snap.Keyboard = nil
		}
	}
	for uid, m := range s.members[chatID] {
		name := fmt.Sprint(uid)
		if u, ok := s.users[uid]; ok {
			name = u.FirstName
		}
		snap.Members = append(snap.Members, MemberView{UserID: uid, Name: name, Status: m.Status})
	}
	sort.Slice(snap.Members, func(i, j int) bool { return snap.Members[i].UserID < snap.Members[j].UserID })
	return snap
}

// ChatList returns all chats.
func (s *Sim) ChatList() []ChatView {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ChatView
	for _, c := range s.chats {
		out = append(out, ChatView{ID: c.ID, Type: c.Type, Title: c.Title})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// PressByID clicks a button on a message identified by chat and id.
func (s *Sim) PressByID(userID, chatID, msgID int64, button string) error {
	s.mu.Lock()
	m, ok := s.msgs[chatID][msgID]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("message %d not found in chat %d", msgID, chatID)
	}
	return s.Press(userID, m, button)
}

// ReplyByID sends a reply to a message identified by id.
func (s *Sim) ReplyByID(chatID, userID, replyTo int64, text string) *Msg {
	return s.sendMsg(chatID, userID, text, nil, replyTo)
}

// HasUser reports whether a user exists.
func (s *Sim) HasUser(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.users[id]
	return ok
}

// UserName returns a user's first name.
func (s *Sim) UserName(id int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[id]; ok {
		return u.FirstName
	}
	return ""
}
