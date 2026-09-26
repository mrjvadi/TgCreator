# TgCreator Runtime

ران‌تایم Go برای اجرای ربات‌های تلگرامی که در وب با drag & drop (سبک n8n) ساخته می‌شوند.
وب یک فایل `workflow.json` خروجی می‌دهد؛ این ران‌تایم آن را **یک‌بار کامپایل** می‌کند و برای هر آپدیت فقط بایت‌کد آماده را اجرا می‌کند.

```
 وب (drag & drop)  ──►  workflow.json  ──►  tgcreator compose  ──►  docker-compose.yml
                                                  │                    (فقط سرویس‌های لازم)
                                                  ▼
                                          tgcreator run  ──►  Telegram Bot API
                                                  │
                                         Redis / Postgres / MySQL / SQLite (فقط در صورت نیاز)
```

## ویژگی‌ها

- **سریع**: عبارت‌ها (`{{ ... }}`) هنگام بارگذاری کامپایل می‌شوند؛ پارامترهای ثابت اصلاً ارزیابی نمی‌شوند. آپدیت‌ها بر اساس چت بین workerها تقسیم می‌شوند (ترتیب پیام‌های یک چت حفظ می‌شود و چت‌های مختلف موازی اجرا می‌شوند). اتصال HTTP به تلگرام keep-alive است و نتیجهٔ بررسی ادمین کش می‌شود.
- **فقط بخش‌های لازم بالا می‌آیند**:
  - در زمان اجرا: اگر workflow نود Redis یا دیتابیس نداشته باشد، هیچ اتصالی باز نمی‌شود.
  - از تلگرام فقط نوع آپدیت‌هایی درخواست می‌شود که triggerها لازم دارند (`allowed_updates`).
  - در زمان build: ماژول‌های استفاده‌نشده با build tag از باینری حذف می‌شوند (۲۱MB در مقابل ۹.۷MB).
- **پشتیبانی از همهٔ قابلیت‌های تلگرام**: هر پارامتری که نود نشناسد مستقیم به Bot API فرستاده می‌شود. با `tg.<method>` یا `telegram.api` هم می‌توان **هر متد** Bot API را صدا زد (حتی متدهای آینده): متن، عکس، ویدیو، صدا، فایل، استیکر، آلبوم، نظرسنجی، بن، محدودسازی، پین، inline mode، پرداخت و…
- **Redis و دیتابیس**: Redis، Postgres، MySQL و SQLite (اختیاری) پشتیبانی می‌شوند و migrationها هنگام شروع اجرا می‌شوند.
- **تولید docker-compose**: فقط سرویس‌هایی که workflow واقعاً استفاده می‌کند در آن قرار می‌گیرند.
- **حالت کاربر (state)**: برای گفت‌وگوهای چندمرحله‌ای (مثلاً «نظرت را بفرست»)، در حافظه یا Redis.

## شروع سریع

```bash
go build -o tgcreator ./cmd/tgcreator

./tgcreator validate -w examples/anti-link/workflow.json
# update types:    edited_message, message
# services:        db:main, redis

# تولید docker-compose.yml کنار workflow
./tgcreator compose -w examples/anti-link/workflow.json --build ../..
cd examples/anti-link && cp ../../.env.example .env   # BOT_TOKEN را پر کنید
docker compose up -d --build
```

اجرای مستقیم بدون Docker:

```bash
BOT_TOKEN=123:abc ./tgcreator run -w examples/menu-bot/workflow.json
```

| دستور | کار |
|---|---|
| `tgcreator run -w wf.json` | اجرای ربات |
| `tgcreator validate -w wf.json` | بررسی فایل و نمایش سرویس‌ها و آپدیت‌های لازم |
| `tgcreator compose -w wf.json [-o file] [--build dir] [--image img]` | تولید docker-compose |
| `tgcreator nodes` | فهرست همهٔ نودها (برای ساخت پالت در وب) |

## فرمت workflow.json

```jsonc
{
  "name": "My bot",
  "version": 1,
  "bot": {
    "token": "${BOT_TOKEN}",          // ${VAR} و ${VAR:-default} پشتیبانی می‌شود
    "mode": "polling",                // یا "webhook"
    "parse_mode": "HTML",             // پیش‌فرض برای send/edit
    "api_url": "",                    // برای Bot API Server شخصی
    "webhook": { "url": "https://x.com/webhook", "listen": ":8080", "path": "/webhook", "secret_token": "..." }
  },
  "runtime": {
    "workers": 16,                    // پیش‌فرض: 4 × CPU
    "max_steps": 1000,                // جلوگیری از حلقهٔ بی‌نهایت
    "state_backend": "memory",        // یا "redis"
    "state_ttl": "24h",
    "health_listen": ":9090",         // GET /healthz
    "drop_pending": false
  },
  "services": {
    "redis": { "url": "${REDIS_URL}" },
    "databases": {
      "main": { "driver": "postgres", "dsn": "${DATABASE_URL}", "migrations": ["CREATE TABLE IF NOT EXISTS ..."] }
    }
  },
  "variables": { "site": "https://example.com" },   // در عبارت‌ها: vars.site
  "nodes": [
    { "id": "start", "type": "trigger.command", "params": { "commands": ["start"] }, "position": [100, 200] }
  ],
  "connections": {
    "start": { "main": ["hello"] }    // مبدأ → خروجی → مقصدها
  }
}
```

- `position` و هر فیلد UI دیگری توسط ران‌تایم نادیده گرفته می‌شود.
- هر نود می‌تواند خروجی `error` داشته باشد؛ اگر وصل نباشد و `continue_on_error: true` باشد، اجرا از `main` ادامه پیدا می‌کند.
- متغیرهای محیطی `TGC_BOT_TOKEN`، `TGC_REDIS_URL` و `TGC_DB_<NAME>_DSN` مقادیر فایل را override می‌کنند (docker-compose تولیدشده از همین‌ها استفاده می‌کند).

### عبارت‌ها (Expressions)

هر رشته می‌تواند `{{ ... }}` داشته باشد. اگر کل رشته فقط یک عبارت باشد، نوع اصلی مقدار حفظ می‌شود (عدد، لیست، شیء).
شرط‌ها (`condition`) عبارت خالی هستند و `{{ }}` نمی‌خواهند. موتور عبارت‌ها [expr-lang](https://expr-lang.org) است. دسترسی به فیلد نبود (مثل `message.reply_to_message.from.id`) خطا نمی‌دهد و `nil` برمی‌گرداند.

| متغیر | توضیح |
|---|---|
| `update` | کل آپدیت خام |
| `type` | نوع آپدیت (`message`، `callback_query`، `chat_member`، …) |
| `payload` | بدنهٔ آپدیت (`update[type]`) |
| `message` | پیام (برای callback، پیامِ دکمه) |
| `callback`, `data` | callback query و data آن |
| `chat`, `from` | چت و کاربر (برای همهٔ انواع آپدیت) |
| `text` | متن یا کپشن پیام (برای inline_query، متن query) |
| `command`, `args`, `args_text` | برای `/lock link` مقدارها `"lock"`، `["link"]` و `"link"` هستند |
| `reply` | پیامی که به آن ریپلای شده |
| `vars` | متغیرهای flow (`variables` و `logic.set`) |
| `nodes.<id>` | خروجی هر نودی که قبلاً اجرا شده (مثلاً `nodes.send1.message_id`) |
| `state` | حالت کاربر فعلی (اگر workflow از `state.*` استفاده کند) |
| `error` | در شاخهٔ `error`: `{error, node}` |
| `bot` | اطلاعات ربات (`getMe`) |
| `env` | متغیرهای محیطی با پیشوند `TGC_VAR_` |

توابع اضافه: `hasLink(message یا متن)`، `hasMention(message)`، `hasEntity(message, "url", ...)`، `mention(user)`، `escapeHTML(s)`، `str(v)`، `toJSON(v)`، `fromJSON(s)`، `coalesce(a, b, ...)`. تمام توابع داخلی expr هم در دسترس‌اند: `lower`، `upper`، `trim`، `split`، `len`، `now()`، `matches`، `contains`، `in`، `??`، `? :`.

### نودها

| نود | خروجی‌ها | توضیح |
|---|---|---|
| `trigger.command` | main | `commands`، `chat_types`، `condition`؛ `/cmd@otherbot` نادیده گرفته می‌شود |
| `trigger.message` | main | `updates`، `chat_types`، `has` (`photo`، `video`، `new_chat_members`، …)، `text`، `contains`، `regex`، `condition` |
| `trigger.callback` | main | `data`، `prefix`، `regex`؛ خروجی: `{data, suffix, matches}` |
| `trigger.update` | main | `on`: هر نوع آپدیت یا `"*"` (مثلاً `chat_member`، `inline_query`، `poll_answer`) |
| `telegram.send_message` | main | `text` + هر پارامتر sendMessage |
| `telegram.send_media` | main | `type`: photo/video/audio/document/animation/voice/video_note/sticker، `file`: file_id، URL یا `file:///path` |
| `telegram.send_media_group` | main | آلبوم |
| `telegram.edit_message` / `edit_buttons` | main | پیش‌فرض: پیامِ دکمه‌ای که زده شده |
| `telegram.delete_message` | main | پیش‌فرض: پیام فعلی |
| `telegram.answer_callback` | main | `text`، `show_alert` |
| `telegram.check_admin` | true / false | با کش؛ ادمین‌های ناشناس (anonymous) هم تشخیص داده می‌شوند |
| `tg.<method>` / `telegram.api` | main | **هر** متد Bot API |
| `logic.if` | true / false | `condition` |
| `logic.switch` | نام case یا default | `value`، `cases` |
| `logic.set` | main | `vars` |
| `logic.delay`، `logic.stop`، `logic.log` | main | |
| `state.set` / `state.clear` | main | حالت هر کاربر در هر چت |
| `redis.get/set/del/incr/command` | main | `redis.incr` با `ttl` برای ضد flood مناسب است |
| `db.query` / `db.exec` | main | `db` (پیش‌فرض `main`)، `query`، `args`، `single` |
| `http.request` | main / error | فراخوانی API بیرونی |

راهنماهای مشترک نودهای ارسال و ویرایش:

- `buttons: [[{text, callback_data | url | web_app ...}]]` کیبورد inline می‌سازد. دکمه‌ای که متنش خالی باشد حذف می‌شود، پس می‌توان دکمه‌ها را شرطی نمایش داد.
- `keyboard: [["A","B"]]` کیبورد معمولی (reply keyboard) می‌سازد.
- `remove_keyboard: true` کیبورد را حذف می‌کند.
- `reply: true` روی پیام فعلی ریپلای می‌کند.
- `chat_id` اگر داده نشود، چت فعلی در نظر گرفته می‌شود.

> **امنیت SQL:** مقادیر را همیشه از طریق `args` بدهید (`$1` در Postgres و `?` در MySQL/SQLite)، نه با `{{ }}` داخل `query`. متن `query` ثابت است و فقط یک بار prepare می‌شود.

## Build tags

| tag | اثر |
|---|---|
| `no_redis` | حذف ماژول Redis |
| `no_postgres` | حذف درایور Postgres |
| `no_mysql` | حذف درایور MySQL |
| `sqlite` | اضافه کردن SQLite (به‌طور پیش‌فرض خاموش است) |

دستور `tgcreator compose --build <dir>` این tagها را خودش از روی workflow محاسبه می‌کند.

## افزودن نود جدید

یک فایل در `internal/nodes/` بسازید و در `init()` آن `engine.Register(engine.NodeType{...})` را صدا بزنید. اگر نود به سرویسی نیاز دارد، `Requires` را برگردانید (مثلاً `"redis"` یا `"db:main"`) و اتصال را در `Init(e *engine.Engine)` بگیرید.

## تست

```bash
go test -race ./...
# تست یکپارچه با Redis و Postgres واقعی:
TGC_REDIS_URL=redis://localhost:6379/0 TGC_DB_MAIN_DSN=postgres://... \
  go test -tags integration ./internal/nodes/
```
