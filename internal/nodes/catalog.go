package nodes

import "github.com/mrjvadi/tgcreator/internal/engine"

// Builder metadata: labels, categories, outputs and parameter editors used
// by the web panel. Redis and database nodes describe themselves in their
// own (build-tagged) files.

var chatTypes = []string{"private", "group", "supergroup", "channel"}

var (
	pChatTypes = engine.Param{Name: "chat_types", Label: "نوع چت", Type: "tags", Options: chatTypes, Help: "خالی = همه"}
	pCondition = engine.Param{Name: "condition", Label: "شرط اضافه", Type: "expr", Placeholder: "from.id != 123", Help: "عبارت بدون {{ }}"}
	pChatID    = engine.Param{Name: "chat_id", Label: "چت مقصد", Type: "text", Placeholder: "{{ chat.id }}", Help: "خالی = چت فعلی"}
	pButtons   = engine.Param{Name: "buttons", Label: "دکمه‌های شیشه‌ای", Type: "buttons"}
	pKeyboard  = engine.Param{Name: "keyboard", Label: "کیبورد پایین صفحه", Type: "keyboard"}
	pReply     = engine.Param{Name: "reply", Label: "ریپلای روی پیام فعلی", Type: "bool"}
	pParseMode = engine.Param{Name: "parse_mode", Label: "قالب متن", Type: "select", Options: []string{"", "HTML", "MarkdownV2"}, Help: "خالی = پیش‌فرض ربات"}
)

func init() {
	// ---- triggers ----
	engine.Describe("trigger.command", engine.Meta{
		Label: "دستور (/start …)", Category: "trigger", Icon: "⌘",
		Params: []engine.Param{
			{Name: "commands", Label: "دستورها", Type: "tags", Required: true, Placeholder: "start", Help: "بدون /"},
			pChatTypes, pCondition,
		},
	})
	engine.Describe("trigger.message", engine.Meta{
		Label: "پیام جدید", Category: "trigger", Icon: "💬",
		Params: []engine.Param{
			{Name: "updates", Label: "نوع آپدیت", Type: "tags", Options: []string{"message", "edited_message", "channel_post", "edited_channel_post", "business_message", "guest_message"}, Help: "پیش‌فرض: message"},
			pChatTypes,
			{Name: "has", Label: "شامل باشد", Type: "tags", Options: []string{"text", "photo", "video", "document", "audio", "voice", "sticker", "animation", "contact", "location", "poll", "new_chat_members", "left_chat_member", "successful_payment", "reply_to_message"}},
			{Name: "text", Label: "متن دقیقاً برابر", Type: "text"},
			{Name: "contains", Label: "متن شامل یکی از", Type: "tags"},
			{Name: "regex", Label: "الگوی Regex", Type: "text", Placeholder: `(?i)^سلام`},
			pCondition,
		},
	})
	engine.Describe("trigger.callback", engine.Meta{
		Label: "کلیک دکمه", Category: "trigger", Icon: "👆",
		Params: []engine.Param{
			{Name: "data", Label: "callback_data دقیق", Type: "text"},
			{Name: "prefix", Label: "شروع callback_data با", Type: "text", Placeholder: "buy:", Help: "بقیه در nodes.<id>.suffix"},
			{Name: "regex", Label: "الگوی Regex", Type: "text"},
			pCondition,
		},
	})
	engine.Describe("trigger.update", engine.Meta{
		Label: "هر نوع آپدیت", Category: "trigger", Icon: "⚡",
		Params: []engine.Param{
			{Name: "on", Label: "نوع آپدیت", Type: "tags", Required: true, Options: []string{"*", "inline_query", "chosen_inline_result", "pre_checkout_query", "shipping_query", "poll", "poll_answer", "my_chat_member", "chat_member", "chat_join_request", "message_reaction", "message_reaction_count", "chat_boost", "removed_chat_boost", "business_connection", "purchased_paid_media", "subscription", "managed_bot"}},
			pCondition,
		},
	})

	// ---- telegram ----
	engine.Describe("telegram.send_message", engine.Meta{
		Label: "ارسال پیام", Category: "telegram", Icon: "✉️", Method: "sendMessage",
		Params: []engine.Param{
			{Name: "text", Label: "متن", Type: "textarea", Required: true, Placeholder: "سلام {{ from.first_name }}"},
			pButtons, pKeyboard, pReply, pParseMode, pChatID,
			{Name: "remove_keyboard", Label: "حذف کیبورد پایین", Type: "bool"},
		},
	})
	engine.Describe("telegram.send_media", engine.Meta{
		Label: "ارسال رسانه", Category: "telegram", Icon: "🖼", Method: "sendPhoto",
		Params: []engine.Param{
			{Name: "type", Label: "نوع", Type: "select", Required: true, Default: "photo", Options: []string{"photo", "video", "audio", "document", "animation", "voice", "video_note", "sticker"}},
			{Name: "file", Label: "فایل", Type: "file", Required: true, Help: "file_id، لینک، یا file:///مسیر"},
			{Name: "caption", Label: "کپشن", Type: "textarea"},
			pButtons, pReply, pParseMode, pChatID,
		},
	})
	engine.Describe("telegram.send_media_group", engine.Meta{
		Label: "ارسال آلبوم", Category: "telegram", Icon: "🗂", Method: "sendMediaGroup",
		Params: []engine.Param{
			{Name: "media", Label: "آیتم‌ها", Type: "json", Required: true, Default: []any{map[string]any{"type": "photo", "media": ""}}, Help: "۲ تا ۱۰ مورد InputMedia"},
			pChatID,
		},
	})
	engine.Describe("telegram.edit_message", engine.Meta{
		Label: "ویرایش پیام", Category: "telegram", Icon: "✏️", Method: "editMessageText",
		Params: []engine.Param{
			{Name: "text", Label: "متن جدید", Type: "textarea", Required: true},
			pButtons, pParseMode,
			{Name: "message_id", Label: "شناسهٔ پیام", Type: "text", Help: "خالی = پیامِ دکمهٔ زده‌شده"},
			pChatID,
		},
	})
	engine.Describe("telegram.edit_buttons", engine.Meta{
		Label: "ویرایش دکمه‌ها", Category: "telegram", Icon: "🔘", Method: "editMessageReplyMarkup",
		Params: []engine.Param{pButtons, {Name: "message_id", Label: "شناسهٔ پیام", Type: "text", Help: "خالی = پیام فعلی"}, pChatID},
	})
	engine.Describe("telegram.delete_message", engine.Meta{
		Label: "حذف پیام", Category: "telegram", Icon: "🗑", Method: "deleteMessage",
		Params: []engine.Param{{Name: "message_id", Label: "شناسهٔ پیام", Type: "text", Help: "خالی = پیام فعلی"}, pChatID},
	})
	engine.Describe("telegram.answer_callback", engine.Meta{
		Label: "پاسخ به کلیک", Category: "telegram", Icon: "💭", Method: "answerCallbackQuery",
		Params: []engine.Param{
			{Name: "text", Label: "متن اعلان", Type: "text"},
			{Name: "show_alert", Label: "به‌صورت پنجره", Type: "bool"},
			{Name: "url", Label: "لینک", Type: "text"},
		},
	})
	engine.Describe("telegram.check_admin", engine.Meta{
		Label: "ادمین است؟", Category: "telegram", Icon: "🛡", Outputs: []string{"true", "false"},
		Params: []engine.Param{
			{Name: "user_id", Label: "کاربر", Type: "text", Placeholder: "{{ from.id }}", Help: "خالی = فرستنده"},
			pChatID,
			{Name: "cache", Label: "مدت کش", Type: "duration", Placeholder: "5m"},
		},
	})
	engine.Describe("telegram.api", engine.Meta{
		Label: "متد دلخواه Bot API", Category: "telegram", Icon: "🔧",
		Params: []engine.Param{
			{Name: "method", Label: "متد", Type: "method", Required: true},
			{Name: "params", Label: "پارامترها", Type: "json", Default: map[string]any{}},
		},
	})
	engine.Describe("tg.", engine.Meta{Label: "متد Bot API", Category: "telegram", Icon: "🔧"})

	// ---- logic ----
	engine.Describe("logic.if", engine.Meta{
		Label: "شرط", Category: "logic", Icon: "⑂", Outputs: []string{"true", "false"},
		Params: []engine.Param{{Name: "condition", Label: "شرط", Type: "expr", Required: true, Placeholder: "text == 'سلام'"}},
	})
	engine.Describe("logic.switch", engine.Meta{
		Label: "انتخاب مسیر", Category: "logic", Icon: "⇶", Outputs: []string{"default"}, CaseOutputs: "cases",
		Params: []engine.Param{
			{Name: "value", Label: "مقدار", Type: "text", Required: true, Placeholder: "{{ data }}"},
			{Name: "cases", Label: "حالت‌ها", Type: "tags", Required: true},
		},
	})
	engine.Describe("logic.set", engine.Meta{
		Label: "تنظیم متغیر", Category: "logic", Icon: "𝑥",
		Params: []engine.Param{{Name: "vars", Label: "متغیرها", Type: "vars", Required: true, Help: "بعداً با vars.نام"}},
	})
	engine.Describe("logic.foreach", engine.Meta{
		Label: "حلقه", Category: "logic", Icon: "↻", Outputs: []string{"item", "done"},
		Params: []engine.Param{
			{Name: "items", Label: "لیست", Type: "text", Required: true, Placeholder: "{{ nodes.users }}", Help: "هر عضو در vars.item"},
			{Name: "delay", Label: "مکث بین موارد", Type: "duration", Placeholder: "40ms"},
		},
	})
	engine.Describe("logic.delay", engine.Meta{
		Label: "مکث", Category: "logic", Icon: "⏱",
		Params: []engine.Param{{Name: "duration", Label: "مدت", Type: "duration", Required: true, Default: "10s"}},
	})
	engine.Describe("logic.stop", engine.Meta{Label: "پایان", Category: "logic", Icon: "⏹", Outputs: []string{}})
	engine.Describe("logic.log", engine.Meta{
		Label: "لاگ", Category: "logic", Icon: "📝",
		Params: []engine.Param{{Name: "message", Label: "پیام", Type: "text", Required: true}},
	})

	// ---- state ----
	engine.Describe("state.set", engine.Meta{
		Label: "ذخیرهٔ وضعیت کاربر", Category: "state", Icon: "📌",
		Params: []engine.Param{
			{Name: "values", Label: "مقادیر", Type: "vars", Required: true, Help: "بعداً با state.نام"},
			{Name: "clear", Label: "پاک کردن قبلی‌ها", Type: "bool"},
		},
	})
	engine.Describe("state.clear", engine.Meta{Label: "پاک کردن وضعیت کاربر", Category: "state", Icon: "🧹"})

	// ---- http ----
	engine.Describe("http.request", engine.Meta{
		Label: "درخواست HTTP", Category: "http", Icon: "🌐",
		Params: []engine.Param{
			{Name: "method", Label: "متد", Type: "select", Default: "GET", Options: []string{"GET", "POST", "PUT", "PATCH", "DELETE"}},
			{Name: "url", Label: "آدرس", Type: "text", Required: true, Placeholder: "https://api.example.com/items"},
			{Name: "query", Label: "Query", Type: "vars"},
			{Name: "headers", Label: "هدرها", Type: "vars"},
			{Name: "body", Label: "بدنه (JSON)", Type: "json"},
			{Name: "timeout", Label: "مهلت", Type: "duration", Placeholder: "10s"},
		},
	})
}
