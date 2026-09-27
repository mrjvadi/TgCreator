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

- **سریع**:
  - عبارت‌ها (`{{ ... }}`) هنگام بارگذاری کامپایل می‌شوند و پارامترهای ثابت اصلاً ارزیابی نمی‌شوند.
  - هر چت فعال صف مخصوص خودش را دارد: ترتیب پیام‌های یک چت حفظ می‌شود و هیچ چتی منتظر چت دیگری نمی‌ماند.
  - اتصال HTTP به تلگرام keep-alive است و نتیجهٔ بررسی ادمین کش می‌شود. جزئیات در بخش [کارایی](#کارایی).
- **فقط بخش‌های لازم بالا می‌آیند**:
  - در زمان اجرا: اگر workflow نود Redis یا دیتابیس نداشته باشد، هیچ اتصالی باز نمی‌شود.
  - از تلگرام فقط نوع آپدیت‌هایی درخواست می‌شود که triggerها لازم دارند (`allowed_updates`).
  - در زمان build: ماژول‌های استفاده‌نشده با build tag از باینری حذف می‌شوند (۲۱MB در مقابل ۹.۷MB).
- **پشتیبانی از همهٔ قابلیت‌های تلگرام**: هر پارامتری که نود نشناسد مستقیم به Bot API فرستاده می‌شود. با `tg.<method>` یا `telegram.api` هم می‌توان **هر متد** Bot API را صدا زد (حتی متدهای آینده): متن، عکس، ویدیو، صدا، فایل، استیکر، آلبوم، نظرسنجی، بن، محدودسازی، پین، inline mode، پرداخت و…
- **Redis و دیتابیس**: Redis، Postgres، MySQL و SQLite (اختیاری) پشتیبانی می‌شوند و migrationها هنگام شروع اجرا می‌شوند.
- **تولید docker-compose**: فقط سرویس‌هایی که workflow واقعاً استفاده می‌کند در آن قرار می‌گیرند.
- **حالت کاربر (state)**: برای گفت‌وگوهای چندمرحله‌ای (مثلاً «نظرت را بفرست»)، در حافظه یا Redis.

## پنل ساخت (وب)

پنل با **React + TypeScript** (در `web/`) ساخته شده است. سرور آن به **Go** نوشته شده و بخشی از همین برنامه است (`tgcreator panel`). ارتباط زنده با مرورگر از طریق **Centrifugo** (WebSocket) انجام می‌شود.

- **بوم drag & drop:** نودها را از فهرست بکشید و خروجی‌ها را به هم وصل کنید. هر نود یک خروجی «خطا» هم دارد. خروجی‌های `true`/`false`، حالت‌های switch و `item`/`done` حلقه جدا نمایش داده می‌شوند.
- **فرم تنظیمات هر نود** خودکار از روی توضیحات نودها ساخته می‌شود:
  - ویرایشگر دکمه‌های شیشه‌ای و کیبورد
  - انتخاب‌گر متغیرها، مثل `{{ from.first_name }}`
  - ویرایشگر شرط، SQL و JSON
- **همهٔ ۱۸۵ متد Bot API:** فرم هر متد مستقیم از مشخصات رسمی تلگرام ساخته می‌شود. «پارامترهای بیشتر تلگرام» هم برای هر نود ارسال در دسترس است.
- **اعتبارسنجی زنده:** با هر تغییر، workflow روی سرور بررسی می‌شود و همهٔ خطاها و هشدارها روی نود مربوط نمایش داده می‌شوند.
- **▶ تست زنده بدون توکن:** workflow روی ران‌تایم واقعی و در یک تلگرام شبیه‌سازی‌شده اجرا می‌شود و رویدادها از طریق Centrifugo به مرورگر می‌رسند.
  - چت‌ها: پیوی شما، پیوی کاربر تست، گروه و کانال.
  - می‌توانید پیام بفرستید، دکمه بزنید، ریپلای کنید، شماره بفرستید و inline query امتحان کنید.
  - Redis برای تست در حافظه ساخته می‌شود و به داده‌های واقعی دست نمی‌زند. برای workflowهایی که دیتابیس دارند، `TGC_TEST_DB_<NAME>_DSN` را به پنل بدهید.
- **امکانات دیگر:**
  - باز کردن نمونه‌ها و فایل‌ها
  - مرتب‌سازی خودکار نودها
  - Undo/Redo با Ctrl+Z
  - ذخیرهٔ خودکار پیش‌نویس در مرورگر
  - خروجی `workflow.json` و `docker-compose.yml`

### اجرای پنل با Docker

```bash
cd deploy/panel
cp .env.example .env     # CENTRIFUGO_SECRET و CENTRIFUGO_API_KEY را عوض کنید
docker compose up -d --build
# http://localhost:8090
```

### اجرای پنل بدون Docker

```bash
# Centrifugo v6
CENTRIFUGO_CLIENT_TOKEN_HMAC_SECRET_KEY=secret CENTRIFUGO_HTTP_API_KEY=apikey \
CENTRIFUGO_CLIENT_ALLOWED_ORIGINS=http://localhost:8090 \
  centrifugo -c deploy/centrifugo/config.json

# پنل
TGC_CENTRIFUGO_API_URL=http://localhost:8000/api TGC_CENTRIFUGO_API_KEY=apikey \
TGC_CENTRIFUGO_SECRET=secret TGC_CENTRIFUGO_WS_URL=ws://localhost:8000/connection/websocket \
  go run ./cmd/tgcreator panel
```

| متغیر محیطی | کار |
|---|---|
| `TGC_CENTRIFUGO_API_URL`، `TGC_CENTRIFUGO_API_KEY` | انتشار رویدادها از سرور با HTTP API سنتریفیوگو |
| `TGC_CENTRIFUGO_SECRET` | امضای JWT اتصال و اشتراک (همان `client.token.hmac_secret_key`) |
| `TGC_CENTRIFUGO_WS_URL` | آدرس WebSocket که مرورگر به آن وصل می‌شود |
| `TGC_PANEL_PASSWORD` | رمز Basic Auth برای کل پنل (اختیاری) |
| `TGC_TEST_DB_<NAME>_DSN` | دیتابیس آزمایشی برای تست workflowهایی که دیتابیس دارند |

بدون Centrifugo هم پنل کار می‌کند و تست زنده به polling برمی‌گردد.

**توسعهٔ رابط کاربری:** در پوشهٔ `web/` دستور `npm ci && npm run dev` را اجرا کنید (Vite درخواست‌های `/api` را به پنل روی پورت 8090 می‌فرستد). آدرس Vite را هم به Centrifugo اضافه کنید؛ مقدارها با فاصله جدا می‌شوند: `CENTRIFUGO_CLIENT_ALLOWED_ORIGINS="http://localhost:8090 http://localhost:5173"`. با `npm run build` خروجی در `web/dist` ساخته می‌شود و داخل فایل اجرایی Go قرار می‌گیرد، پس برای اجرای پنل به Node نیازی نیست.

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
| `tgcreator nodes [-json]` | فهرست همهٔ نودها با برچسب‌ها و پارامترها |
| `tgcreator panel [-addr :8090]` | اجرای پنل ساخت |

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
    "workers": 512,                   // تعداد آپدیت هم‌زمان (پیش‌فرض ۵۱۲)
    "queue_size": 100000,             // حداکثر آپدیت در صف؛ اگر پر شود، دریافت آپدیت موقتاً متوقف می‌شود
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
| `logic.foreach` | item / done | `items`، `delay`؛ برای هر عضو `vars.item` و `vars.index` (مثلاً ارسال همگانی) |
| `logic.delay` | main | ادامهٔ شاخه بعد از `duration` اجرا می‌شود، **بدون** این‌که پیام‌های دیگر چت منتظر بمانند (مثلاً حذف خودکار هشدار بعد از ۱۰ ثانیه) |
| `logic.stop`، `logic.log` | main | |
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

## کارایی

اندازه‌گیری روی ۴ هستهٔ CPU (Xeon 2.8GHz). کد اندازه‌گیری‌ها در `internal/bench` است.

**هزینهٔ خود ران‌تایم برای هر آپدیت** (بدون شبکه؛ یک میکروثانیه یک‌میلیونیم ثانیه است):

| کار | زمان |
|---|---|
| پیام گروه که هیچ اقدامی لازم ندارد (۴ trigger بررسی می‌شوند) | ۸ میکروثانیه |
| `/start` و ارسال پاسخ با دکمه | ۲۱ میکروثانیه |
| لینک در گروه: بررسی ادمین، حذف و هشدار | ۲۹ میکروثانیه |
| ۳۰ نود منطقی و یک پاسخ | ۴۷ میکروثانیه |
| Redis (۲ دستور) + درج در Postgres + پاسخ | ۲۲۰ میکروثانیه |

**زیر بار** (حلقهٔ واقعی polling؛ p99 یعنی ۹۹٪ آپدیت‌ها سریع‌تر از این عدد انجام شده‌اند):

| سناریو | آپدیت در ثانیه | p50 | p99 |
|---|---|---|---|
| ۱۰۰٬۰۰۰ پیام در ۱٬۰۰۰ گروه، بدون تأخیر شبکه | ~۷۰٬۰۰۰ | | |
| ۱۰٬۰۰۰ `/start` با ۱۰۰ms تأخیر تا تلگرام | ~۴٬۹۰۰ | ۱ ثانیه | ۲ ثانیه |
| ۱۰٬۰۰۰ `/start` از ۱۰٬۰۰۰ کاربر، ۱۰۰ms، `workers: 2048` | ~۱۷٬۰۰۰ | ۰٫۳ ثانیه | ۰٫۵ ثانیه |

در عمل گلوگاه خود تلگرام است:
- **تأخیر شبکه تا سرور تلگرام** (معمولاً ۵۰ تا ۳۰۰ میلی‌ثانیه برای هر درخواست).
- **محدودیت‌های ارسال تلگرام:** حدود ۳۰ پیام در ثانیه برای ارسال همگانی، ۱ پیام در ثانیه در هر چت، و ۲۰ پیام در دقیقه در هر گروه.

ران‌تایم خطای 429 تلگرام را با `retry_after` مدیریت می‌کند.

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
```

### شبیه‌ساز تلگرام (`internal/tgsim`)

برای تست بدون ربات واقعی، یک Bot API ساختگی داریم که:

- **هر درخواست را با مشخصات رسمی Bot API مقایسه می‌کند** (`internal/tgsim/botapi.json`، نسخهٔ 10.3). نام متد، پارامترهای اجباری، پارامترهای ناشناخته، نوع هر مقدار (به‌صورت بازگشتی در اشیای تو در تو) و فایل‌های `attach://` بررسی می‌شوند.
- **مثل تلگرام واقعی رفتار می‌کند**:
  - چت‌ها، اعضا، سطح دسترسی‌ها، پیام‌ها، حذف و ویرایش را نگه می‌دارد.
  - HTML را اعتبارسنجی می‌کند.
  - همان خطاهای تلگرام را برمی‌گرداند، مثل `bot was blocked by the user`، `message can't be deleted`، `message is not modified`، `not enough rights` و `query is too old`.
  - `allowed_updates` را رعایت می‌کند.
- آزمون `TestEveryBotAPIMethod` **همهٔ متدهای** Bot API را از طریق ران‌تایم صدا می‌زند (۱۸۴ متد به‌علاوهٔ `getUpdates` که خود ران‌تایم استفاده می‌کند، همراه با آپلود فایل) و انتظار صفر خطای مشخصات دارد.
- آزمون `TestScenarioCommunityBot` نمونهٔ `examples/community-bot` را با ۶ کاربر، سوپرگروه، گروه معمولی، کانال، inline mode و پرداخت Stars روی ران‌تایم واقعی (long polling) و Redis و Postgres واقعی اجرا می‌کند. گزارش گفت‌وگو را در [examples/community-bot/scenario-transcript.md](examples/community-bot/scenario-transcript.md) ببینید.
- آزمون `TestWebhookMenuBot` حالت webhook را تست می‌کند، از جمله رد شدن درخواست جعلی.

```bash
# سناریو (جدول‌های دیتابیس تست و کلیدهای Redis آن پاک می‌شوند):
TGC_TEST_REDIS_URL=redis://localhost:6379/15 \
TGC_TEST_DB_DSN=postgres://user@localhost/scenario?sslmode=disable \
TGC_SIM_TRANSCRIPT=/tmp/transcript \
  go test -race -run Scenario -v ./internal/tgsim/

# تست یکپارچهٔ نمونهٔ anti-link:
TGC_REDIS_URL=redis://localhost:6379/0 TGC_DB_MAIN_DSN=postgres://... \
  go test -tags integration ./internal/nodes/
```

به‌روزرسانی مشخصات Bot API: فایل `api.json` از [telegram-bot-api-spec](https://github.com/PaulSonOfLars/telegram-bot-api-spec) را بگیرید و `internal/tgsim/botapi.json` را جایگزین کنید. آزمون `TestRuntimeKnowsEveryUpdateType` هر نوع آپدیت جدید را گزارش می‌کند.
