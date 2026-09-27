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
		Label: "دستور ربات", Category: "trigger", Icon: "square-terminal", Summary: "وقتی کاربر دستوری مثل /start بفرستد",
		Params: []engine.Param{
			{Name: "commands", Label: "دستورها", Type: "tags", Required: true, Placeholder: "start", Help: "بدون /"},
			pChatTypes, pCondition,
		},
	})
	engine.Describe("trigger.message", engine.Meta{
		Label: "پیام جدید", Category: "trigger", Icon: "message-square", Summary: "هر پیام جدید؛ با فیلتر متن، نوع چت و محتوا",
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
		Label: "کلیک دکمه", Category: "trigger", Icon: "mouse-pointer-click", Summary: "وقتی کاربر روی دکمهٔ شیشه‌ای بزند",
		Params: []engine.Param{
			{Name: "data", Label: "callback_data دقیق", Type: "text"},
			{Name: "prefix", Label: "شروع callback_data با", Type: "text", Placeholder: "buy:", Help: "بقیه در nodes.<id>.suffix"},
			{Name: "regex", Label: "الگوی Regex", Type: "text"},
			pCondition,
		},
	})
	engine.Describe("trigger.update", engine.Meta{
		Label: "هر نوع آپدیت", Category: "trigger", Icon: "zap", Summary: "هر آپدیت دیگر: inline، پرداخت، عضویت، نظرسنجی…",
		Params: []engine.Param{
			{Name: "on", Label: "نوع آپدیت", Type: "tags", Required: true, Options: []string{"*", "inline_query", "chosen_inline_result", "pre_checkout_query", "shipping_query", "poll", "poll_answer", "my_chat_member", "chat_member", "chat_join_request", "message_reaction", "message_reaction_count", "chat_boost", "removed_chat_boost", "business_connection", "purchased_paid_media", "subscription", "managed_bot"}},
			pCondition,
		},
	})

	// ---- telegram ----
	engine.Describe("telegram.send_message", engine.Meta{
		Label: "ارسال پیام", Category: "telegram", Icon: "send", Summary: "متن با دکمهٔ شیشه‌ای یا کیبورد", Method: "sendMessage",
		Params: []engine.Param{
			{Name: "text", Label: "متن", Type: "textarea", Required: true, Placeholder: "سلام {{ from.first_name }}"},
			pButtons, pKeyboard, pReply, pParseMode, pChatID,
			{Name: "remove_keyboard", Label: "حذف کیبورد پایین", Type: "bool"},
		},
	})
	engine.Describe("telegram.send_media", engine.Meta{
		Label: "ارسال رسانه", Category: "telegram", Icon: "image", Summary: "عکس، ویدیو، فایل، صدا، استیکر و…", Method: "sendPhoto",
		Params: []engine.Param{
			{Name: "type", Label: "نوع", Type: "select", Required: true, Default: "photo", Options: []string{"photo", "video", "audio", "document", "animation", "voice", "video_note", "sticker"}},
			{Name: "file", Label: "فایل", Type: "file", Required: true, Help: "file_id، لینک، یا file:///مسیر"},
			{Name: "caption", Label: "کپشن", Type: "textarea"},
			pButtons, pReply, pParseMode, pChatID,
		},
	})
	engine.Describe("telegram.send_media_group", engine.Meta{
		Label: "ارسال آلبوم", Category: "telegram", Icon: "images", Summary: "چند عکس یا ویدیو در یک آلبوم", Method: "sendMediaGroup",
		Params: []engine.Param{
			{Name: "media", Label: "آیتم‌ها", Type: "json", Required: true, Default: []any{map[string]any{"type": "photo", "media": ""}}, Help: "۲ تا ۱۰ مورد InputMedia"},
			pChatID,
		},
	})
	engine.Describe("telegram.edit_message", engine.Meta{
		Label: "ویرایش پیام", Category: "telegram", Icon: "pencil", Summary: "تغییر متن و دکمه‌های یک پیام", Method: "editMessageText",
		Params: []engine.Param{
			{Name: "text", Label: "متن جدید", Type: "textarea", Required: true},
			pButtons, pParseMode,
			{Name: "message_id", Label: "شناسهٔ پیام", Type: "text", Help: "خالی = پیامِ دکمهٔ زده‌شده"},
			pChatID,
		},
	})
	engine.Describe("telegram.edit_buttons", engine.Meta{
		Label: "ویرایش دکمه‌ها", Category: "telegram", Icon: "keyboard", Summary: "فقط دکمه‌های یک پیام را عوض می‌کند", Method: "editMessageReplyMarkup",
		Params: []engine.Param{pButtons, {Name: "message_id", Label: "شناسهٔ پیام", Type: "text", Help: "خالی = پیام فعلی"}, pChatID},
	})
	engine.Describe("telegram.delete_message", engine.Meta{
		Label: "حذف پیام", Category: "telegram", Icon: "trash-2", Summary: "حذف پیام فعلی یا هر پیام دیگر", Method: "deleteMessage",
		Params: []engine.Param{{Name: "message_id", Label: "شناسهٔ پیام", Type: "text", Help: "خالی = پیام فعلی"}, pChatID},
	})
	engine.Describe("telegram.answer_callback", engine.Meta{
		Label: "پاسخ به کلیک", Category: "telegram", Icon: "bell", Summary: "اعلان یا پنجرهٔ کوچک بعد از زدن دکمه", Method: "answerCallbackQuery",
		Params: []engine.Param{
			{Name: "text", Label: "متن اعلان", Type: "text"},
			{Name: "show_alert", Label: "به‌صورت پنجره", Type: "bool"},
			{Name: "url", Label: "لینک", Type: "text"},
		},
	})
	engine.Describe("telegram.check_admin", engine.Meta{
		Label: "ادمین است؟", Category: "telegram", Icon: "shield-check", Summary: "ادمین بودن کاربر را بررسی می‌کند (با کش)", Outputs: []string{"true", "false"},
		Params: []engine.Param{
			{Name: "user_id", Label: "کاربر", Type: "text", Placeholder: "{{ from.id }}", Help: "خالی = فرستنده"},
			pChatID,
			{Name: "cache", Label: "مدت کش", Type: "duration", Placeholder: "5m"},
		},
	})
	engine.Describe("telegram.api", engine.Meta{
		Label: "متد دلخواه Bot API", Category: "telegram", Icon: "braces", Summary: "هر متد Bot API با پارامترهای دلخواه",
		Params: []engine.Param{
			{Name: "method", Label: "متد", Type: "method", Required: true},
			{Name: "params", Label: "پارامترها", Type: "json", Default: map[string]any{}},
		},
	})
	engine.Describe("tg.", engine.Meta{Label: "متد Bot API", Category: "telegram", Icon: "plug", Summary: "متد Bot API"})

	// ---- logic ----
	engine.Describe("logic.if", engine.Meta{
		Label: "شرط", Category: "logic", Icon: "git-branch", Summary: "دو مسیر بر اساس یک شرط", Outputs: []string{"true", "false"},
		Params: []engine.Param{{Name: "condition", Label: "شرط", Type: "expr", Required: true, Placeholder: "text == 'سلام'"}},
	})
	engine.Describe("logic.switch", engine.Meta{
		Label: "انتخاب مسیر", Category: "logic", Icon: "split", Summary: "چند مسیر بر اساس یک مقدار", Outputs: []string{"default"}, CaseOutputs: "cases",
		Params: []engine.Param{
			{Name: "value", Label: "مقدار", Type: "text", Required: true, Placeholder: "{{ data }}"},
			{Name: "cases", Label: "حالت‌ها", Type: "tags", Required: true},
		},
	})
	engine.Describe("logic.set", engine.Meta{
		Label: "تنظیم متغیر", Category: "logic", Icon: "variable", Summary: "ذخیرهٔ مقدار در متغیرهای همین اجرا",
		Params: []engine.Param{{Name: "vars", Label: "متغیرها", Type: "vars", Required: true, Help: "بعداً با vars.نام"}},
	})
	engine.Describe("logic.foreach", engine.Meta{
		Label: "حلقه", Category: "logic", Icon: "repeat", Summary: "تکرار برای هر عضو یک لیست", Outputs: []string{"item", "done"},
		Params: []engine.Param{
			{Name: "items", Label: "لیست", Type: "text", Required: true, Placeholder: "{{ nodes.users }}", Help: "هر عضو در vars.item"},
			{Name: "delay", Label: "مکث بین موارد", Type: "duration", Placeholder: "40ms"},
		},
	})
	engine.Describe("logic.delay", engine.Meta{
		Label: "مکث", Category: "logic", Icon: "timer", Summary: "ادامه بعد از مکث، بدون معطل کردن بقیه",
		Params: []engine.Param{{Name: "duration", Label: "مدت", Type: "duration", Required: true, Default: "10s"}},
	})
	engine.Describe("logic.stop", engine.Meta{Label: "پایان", Category: "logic", Icon: "circle-stop", Summary: "پایان این مسیر", Outputs: []string{}})
	engine.Describe("logic.log", engine.Meta{
		Label: "لاگ", Category: "logic", Icon: "scroll-text", Summary: "نوشتن در لاگ ران‌تایم",
		Params: []engine.Param{{Name: "message", Label: "پیام", Type: "text", Required: true}},
	})

	// ---- state ----
	engine.Describe("state.set", engine.Meta{
		Label: "ذخیرهٔ وضعیت کاربر", Category: "state", Icon: "bookmark", Summary: "مرحلهٔ گفت‌وگوی کاربر را به خاطر می‌سپارد",
		Params: []engine.Param{
			{Name: "values", Label: "مقادیر", Type: "vars", Required: true, Help: "بعداً با state.نام"},
			{Name: "clear", Label: "پاک کردن قبلی‌ها", Type: "bool"},
		},
	})
	engine.Describe("state.clear", engine.Meta{Label: "پاک کردن وضعیت کاربر", Category: "state", Icon: "eraser", Summary: "وضعیت کاربر را پاک می‌کند"})

	// ---- http ----
	engine.Describe("http.request", engine.Meta{
		Label: "درخواست HTTP", Category: "http", Icon: "globe", Summary: "فراخوانی یک API بیرونی",
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
