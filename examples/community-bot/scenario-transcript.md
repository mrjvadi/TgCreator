# سناریوی تست‌شده: ربات جامعهٔ گوفرها

این گزارش به‌صورت خودکار توسط `TestScenarioCommunityBot` تولید شده است. همهٔ درخواست‌ها با مشخصات Bot API 10.3 اعتبارسنجی شده‌اند.

## ۱. ربات به گروه‌ها و کانال اضافه می‌شود

- **[Gophers IR]** Ali: ➕ added the bot as administrator
- **[Gophers IR]** 🤖 bot: #1 سلام! 👋 من ربات مدیریت گوفرها هستم. ⏎ برای کار کامل (قفل لینک، ضد اسپم، اخطار) مرا ادمین کنید.
- **[Family]** Ali: ➕ added the bot as member
- **[Family]** 🤖 bot: #1 سلام! 👋 من ربات مدیریت گوفرها هستم. ⏎ برای کار کامل (قفل لینک، ضد اسپم، اخطار) مرا ادمین کنید.
- **[Gopher News]** Ali: ➕ added the bot as administrator

## ۲. علی: /start، منو، عکس، آلبوم و لینک دعوت

- **[💬 Ali]** Ali: #1 /start
- **[💬 Ali]** 🤖 bot: #2 سلام Ali 👋 ⏎ به ربات جامعهٔ گوفرها خوش آمدی. یکی از گزینه‌ها را انتخاب کن:  [📷 عکس خوش‌آمد] [🎞 آلبوم] [✍️ ارسال نظر] [⭐️ اشتراک ویژه] [🔗 لینک دعوت] [🌐 سایت]
- **[💬 Ali]** Ali: 👆 pressed [📷 عکس خوش‌آمد]
- **[💬 Ali]** 🤖 bot: #3 📷 photo (uploaded welcome.jpg) — 🐹 به جمع گوفرهای ایران خوش آمدی!  [🔙 بازگشت به منو]
- **[💬 Ali]** Ali: 👆 pressed [🔙 بازگشت به منو]
- **[💬 Ali]** 🤖 bot: 🗑 deleted #3 (bot)
- **[💬 Ali]** 🤖 bot: #4 سلام Ali 👋 ⏎ به ربات جامعهٔ گوفرها خوش آمدی. یکی از گزینه‌ها را انتخاب کن:  [📷 عکس خوش‌آمد] [🎞 آلبوم] [✍️ ارسال نظر] [⭐️ اشتراک ویژه] [🔗 لینک دعوت] [🌐 سایت]
- **[💬 Ali]** Ali: 👆 pressed [🎞 آلبوم]
- **[💬 Ali]** 🤖 bot: 💬 toast to Ali: در حال ارسال آلبوم…
- **[💬 Ali]** 🤖 bot: #5 📷 album photo (uploaded (attach://file1))
- **[💬 Ali]** 🤖 bot: #6 📷 album photo (from URL https://go.dev/images/gophers/ladder.png)
- **[💬 Ali]** 🤖 bot: #7 🎬 album video (from URL https://go.dev/images/gophers/intro.mp4)
- **[💬 Ali]** Ali: 👆 pressed [🔗 لینک دعوت]
- **[💬 Ali]** 🤖 bot: 💬 alert to Ali: 🔗 لینک دعوت اختصاصی شما: ⏎ https://t.me/gopher_helper_bot?start=ref_101

## ۳. سارا با لینک دعوت علی وارد می‌شود و نظر می‌دهد

- **[💬 Sara]** Sara: #1 /start ref_101
- **[💬 Sara]** 🤖 bot: #2 سلام Sara 👋 ⏎ به ربات جامعهٔ گوفرها خوش آمدی. یکی از گزینه‌ها را انتخاب کن:  [📷 عکس خوش‌آمد] [🎞 آلبوم] [✍️ ارسال نظر] [⭐️ اشتراک ویژه] [🔗 لینک دعوت] [🌐 سایت]
- **[💬 Ali]** 🤖 bot: #8 🎉 Sara با لینک دعوت شما عضو ربات شد!
- **[💬 Sara]** Sara: 👆 pressed [✍️ ارسال نظر]
- **[💬 Sara]** 🤖 bot: ✏️ edited #2 → ✍️ نظرت را در یک پیام بنویس و بفرست.  [❌ لغو]
- **[💬 Sara]** Sara: #3 ربات عالیه <3 فقط دکمه‌های بیشتر & تم تیره!
- **[💬 Sara]** 🤖 bot: #4 🙏 ممنون! نظرت ثبت شد.
- **[💬 Sara]** 🤖 bot: ✏️ edited buttons of #2
- **[💬 Ali]** 🤖 bot: #9 📝 نظر جدید از Sara: ⏎ ربات عالیه <3 فقط دکمه‌های بیشتر & تم تیره!
- **[💬 Sara]** Sara: #5 سلام دوباره

## ۴. سارا اشتراک ویژه می‌خرد (Telegram Stars)

- **[💬 Sara]** Sara: #6 /start
- **[💬 Sara]** 🤖 bot: #7 سلام Sara 👋 ⏎ به ربات جامعهٔ گوفرها خوش آمدی. یکی از گزینه‌ها را انتخاب کن:  [📷 عکس خوش‌آمد] [🎞 آلبوم] [✍️ ارسال نظر] [⭐️ اشتراک ویژه] [🔗 لینک دعوت] [🌐 سایت]
- **[💬 Sara]** Sara: 👆 pressed [⭐️ اشتراک ویژه]
- **[💬 Sara]** 🤖 bot: #8 🧾 invoice «اشتراک ویژهٔ گوفرها» 50 XTR
- **[💬 Sara]** Sara: 💳 checkout 10 XTR (vip:102)
- **[💬 Sara]** 🤖 bot: ❌ payment rejected: مبلغ یا سفارش نامعتبر است.
- **[💬 Sara]** Sara: 💳 checkout 50 XTR (vip:102)
- **[💬 Sara]** 🤖 bot: ✅ payment approved
- **[💬 Sara]** Sara: #9 ✅ paid 50 XTR
- **[💬 Sara]** 🤖 bot: #10 ✅ پرداخت 50 ⭐️ انجام شد. اشتراک ویژهٔ شما فعال است!

## ۵. سارا شماره‌اش را با دکمهٔ کیبورد می‌فرستد

- **[💬 Sara]** Sara: #11 /contact
- **[💬 Sara]** 🤖 bot: #12 برای تکمیل پروفایل، شماره‌ات را با دکمهٔ زیر بفرست:  ⌨️ keyboard: 📱 ارسال شماره من
- **[💬 Sara]** Sara: #13 👤 shared contact +989121234567
- **[💬 Sara]** 🤖 bot: #14 ✅ شماره +989121234567 ثبت شد.  (keyboard removed)

## ۶. رضا ربات را استارت می‌کند و بعد بلاکش می‌کند

- **[💬 Reza]** Reza: #1 /start
- **[💬 Reza]** 🤖 bot: #2 سلام Reza 👋 ⏎ به ربات جامعهٔ گوفرها خوش آمدی. یکی از گزینه‌ها را انتخاب کن:  [📷 عکس خوش‌آمد] [🎞 آلبوم] [✍️ ارسال نظر] [⭐️ اشتراک ویژه] [🔗 لینک دعوت] [🌐 سایت]

## ۷. ندا وارد گروه می‌شود: بی‌صدا + کپچا

- **[Gophers IR]** Neda: ➕ joined the group
- **[Gophers IR]** 🤖 bot: 🔇 restricted Neda
- **[Gophers IR]** 🤖 bot: #3 👋 Neda به Gophers IR خوش آمدی! ⏎ برای فعال شدن ارسال پیام، دکمهٔ زیر را بزن.  [✅ من ربات نیستم]
- **[Gophers IR]** Neda: 🚫 tried to write but can't (restricted): سلام!
- **[Gophers IR]** Mehdi: 👆 pressed [✅ من ربات نیستم]
- **[Gophers IR]** 🤖 bot: 💬 alert to Mehdi: ⛔️ این دکمه مال تو نیست!
- **[Gophers IR]** Neda: 👆 pressed [✅ من ربات نیستم]
- **[Gophers IR]** 🤖 bot: 🔓 lifted restrictions of Neda
- **[Gophers IR]** 🤖 bot: 💬 toast to Neda: ✅ تأیید شدی، خوش آمدی!
- **[Gophers IR]** 🤖 bot: 🗑 deleted #3 (bot)

## ۸. قفل لینک: مهدی اجازه ندارد، سارا (ادمین) قفل می‌کند

- **[Gophers IR]** Mehdi: #4 /lock link
- **[Gophers IR]** 🤖 bot: #5 ⛔️ فقط ادمین‌ها می‌توانند قفل‌ها را تغییر دهند.
- **[Gophers IR]** Sara: #6 /lock link
- **[Gophers IR]** 🤖 bot: #7 🔒 ارسال لینک در این گروه قفل شد.

## ۹. مهدی لینک تبلیغاتی می‌فرستد؛ سارا لینک مجاز

- **[Gophers IR]** Mehdi: #8 سلام به همه
- **[Gophers IR]** Mehdi: #9 عضو کانال ما شوید: t.me/spam_channel
- **[Gophers IR]** 🤖 bot: 🗑 deleted #9 (Mehdi)
- **[Gophers IR]** 🤖 bot: #10 🚫 Mehdi، ارسال لینک در این گروه ممنوع است.
- **[Gophers IR]** 🤖 bot: 🗑 deleted #10 (bot)
- **[Gophers IR]** Sara: #11 مستندات رسمی: https://go.dev/doc
- **[Gophers IR]** Mehdi: ✏️ edited #8: سلام به همه، اینجا رو ببینید www.cheap-ads.com
- **[Gophers IR]** 🤖 bot: 🗑 deleted #8 (Mehdi)
- **[Gophers IR]** 🤖 bot: #12 🚫 Mehdi، ارسال لینک در این گروه ممنوع است.
- **[Gophers IR]** 🤖 bot: 🗑 deleted #12 (bot)

## ۱۰. ضد اسپم: مهدی پشت‌سرهم پیام می‌دهد و بی‌صدا می‌شود

- **[Gophers IR]** Mehdi: #13 خرید فالوور ارزان !
- **[Gophers IR]** Mehdi: #14 خرید فالوور ارزان !!
- **[Gophers IR]** Mehdi: #15 خرید فالوور ارزان !!!
- **[Gophers IR]** Mehdi: #16 خرید فالوور ارزان !!!!
- **[Gophers IR]** 🤖 bot: 🔇 restricted Mehdi for 1h0m0s
- **[Gophers IR]** 🤖 bot: #17 🔇 Mehdi به دلیل ارسال پیام‌های پشت‌سرهم برای ۱ ساعت بی‌صدا شد.
- **[Gophers IR]** Mehdi: 🚫 tried to write but can't (restricted): چرا؟

## ۱۱. اخطار: موارد نامعتبر و سه اخطار به رضا تا اخراج

- **[Gophers IR]** Reza: #18 یه سوال داشتم دربارهٔ goroutine ها
- **[Gophers IR]** Sara: #19 بپرس
- **[Gophers IR]** Reza: #20 ↩️ reply to #19: /warn
- **[Gophers IR]** 🤖 bot: #21 ⛔️ فقط ادمین‌ها می‌توانند اخطار بدهند.
- **[Gophers IR]** Sara: #22 /warn
- **[Gophers IR]** 🤖 bot: #23 ↩️ روی پیام کاربر ریپلای کن و /warn بفرست.
- **[Gophers IR]** Ali: #24 من هم هستم
- **[Gophers IR]** Sara: #25 ↩️ reply to #24: /warn
- **[Gophers IR]** 🤖 bot: #26 ❌ نمی‌توان به ادمین اخطار داد.
- **[Gophers IR]** Sara: #27 ↩️ reply to #18: /warn
- **[Gophers IR]** 🤖 bot: #28 ⚠️ Reza اخطار گرفت (1/3).
- **[Gophers IR]** Sara: #29 ↩️ reply to #18: /warn
- **[Gophers IR]** 🤖 bot: #30 ⚠️ Reza اخطار گرفت (2/3).
- **[Gophers IR]** Sara: #31 ↩️ reply to #18: /warn
- **[Gophers IR]** 🤖 bot: ⛔️ banned Reza
- **[Gophers IR]** 🤖 bot: #32 ⛔️ Reza پس از 3 اخطار از گروه اخراج شد.

## ۱۲. سنجاق، نظرسنجی و رأی‌ها

- **[Gophers IR]** Sara: #33 📣 جلسهٔ ماهانه پنج‌شنبه ساعت ۱۸
- **[Gophers IR]** Sara: #34 ↩️ reply to #33: /pin
- **[Gophers IR]** 🤖 bot: 📌 pinned #33
- **[Gophers IR]** 🤖 bot: #35 📌 سنجاق شد.
- **[Gophers IR]** Ali: #36 /poll زبان مورد علاقه‌ات برای بک‌اند؟
- **[Gophers IR]** 🤖 bot: #37 📊 poll «زبان مورد علاقه‌ات برای بک‌اند؟» [Go 🐹 | Rust 🦀 | Python 🐍]
- **[Gophers IR]** Sara: 🗳 voted option 0
- **[Gophers IR]** 🤖 bot: #38 🗳 Sara رأی داد.
- **[Gophers IR]** Neda: 🗳 voted option 1
- **[Gophers IR]** 🤖 bot: #39 🗳 Neda رأی داد.
- **[Gophers IR]** Sara: reacted 🔥 to #33
- ⚪️ _message_reaction not delivered: the bot did not ask for it in allowed_updates_

## ۱۳. امید درخواست عضویت می‌دهد

- **[Gophers IR]** Omid: 🙋 requested to join
- **[💬 Omid]** 🤖 bot: #1 ✅ درخواست عضویت شما در «Gophers IR» تأیید شد. خوش آمدی!
- **[Gophers IR]** 🤖 bot: ✅ approved join request of Omid

## ۱۴. گروه Family: ربات ادمین نیست

- **[Family]** Ali: #2 /lock link
- **[Family]** 🤖 bot: #3 🔒 ارسال لینک در این گروه قفل شد.
- **[Family]** Reza: #4 این فیلم رو ببینید https://youtu.be/xyz
- ❗️ `deleteMessage → Bad Request: message can't be deleted`
- **[Family]** 🤖 bot: #5 ⚠️ برای حذف لینک‌ها باید مرا ادمین کنید (دسترسی حذف پیام).
- **[Family]** Neda: ➕ joined the group
- **[Family]** 🤖 bot: #7 👋 Neda خوش آمدی!
- **[Family]** Reza: #8 /dice
- **[Family]** 🤖 bot: #9 🎲 dice → 4

## ۱۵. کانال: پست جدید ← ری‌اکشن و دکمهٔ گفت‌وگو

- **[Gopher News]** Gopher News: 🚀 نسخهٔ جدید Go منتشر شد!
- **[Gopher News]** 🤖 bot: reacted 👍 to #1
- **[Gopher News]** 🤖 bot: ✏️ edited buttons of #1  [💬 گفت‌وگو در گروه]

## ۱۶. حالت inline: سارا در یک چت دیگر @gopher_helper_bot goroutine تایپ می‌کند

- **[inline mode]** Sara: ⌨️ @gopher_helper_bot goroutine
- **[inline mode]** 🤖 bot: inline results: 📘 مستندات Go: goroutine | ▶️ Go Playground

## ۱۷. علی: آمار، ارسال همگانی (رضا بلاک کرده) و آمار دوباره

- **[💬 Sara]** Sara: #15 /stats
- **[💬 Ali]** Ali: #10 /stats
- **[💬 Ali]** 🤖 bot: #11 📊 آمار ربات ⏎ 👥 کاربران: 3 (دعوتی: 1) ⏎ 🚫 ربات را بلاک کرده‌اند: 0 ⏎ 📝 نظرها: 1 ⏎ 🧾 سفارش‌ها: 1 (50 ⭐️)
- **[💬 Ali]** Ali: #12 /broadcast نسخهٔ ۲ ربات منتشر شد 🎉
- **[💬 Ali]** 🤖 bot: #13 📢 نسخهٔ ۲ ربات منتشر شد 🎉
- **[💬 Sara]** 🤖 bot: #16 📢 نسخهٔ ۲ ربات منتشر شد 🎉
- ❗️ `sendMessage → Forbidden: bot was blocked by the user`
- **[💬 Ali]** 🤖 bot: #14 📢 ارسال همگانی تمام شد: ✅ 2 موفق، ❌ 1 ناموفق
- **[💬 Ali]** Ali: #15 /stats
- **[💬 Ali]** 🤖 bot: #16 📊 آمار ربات ⏎ 👥 کاربران: 3 (دعوتی: 1) ⏎ 🚫 ربات را بلاک کرده‌اند: 1 ⏎ 📝 نظرها: 1 ⏎ 🧾 سفارش‌ها: 1 (50 ⭐️)

## ۱۸. مهدی در پیوی /help می‌زند

- **[💬 Mehdi]** Mehdi: #1 /help
- **[💬 Mehdi]** 🤖 bot: #2 راهنما ⏎ /start — منوی اصلی ⏎ /contact — ثبت شماره ⏎  ⏎ در گروه: ⏎ /lock link — قفل لینک (ادمین) ⏎ /warn — اخطار با ریپلای (ادمین) ⏎ /pin — سنجاق با ریپلای (ادمین) ⏎ /poll — نظرسنجی ⏎ /dice — تاس
