import { Activity, AtSign, Bot, CornerDownLeft, Link2, Megaphone, Phone, Plus, Reply, RotateCcw, Send, User, UserPlus, Users, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";
import { follow, type LiveMessage, type LiveStatus } from "../realtime";
import type { ChatSnapshot, MsgView, SimEvent, TestSession, Workflow } from "../types";

const ME = 1001;
const OTHER = 1002;

const statusText: Record<LiveStatus, string> = {
  connecting: "در حال اتصال…",
  live: "آنلاین · زنده از طریق Centrifugo",
  polling: "آنلاین · بدون Centrifugo",
  offline: "قطع شد",
};

const mediaLabel: Record<string, string> = {
  photo: "📷 عکس", video: "🎬 ویدیو", audio: "🎵 صدا", document: "📄 فایل", animation: "🎞 گیف", voice: "🎤 پیام صوتی",
  video_note: "⏺ ویدیو مسیج", sticker: "استیکر", poll: "📊 نظرسنجی", dice: "🎲 تاس", invoice: "🧾 صورت‌حساب",
  contact: "👤 مخاطب", location: "📍 موقعیت",
};

function chatMeta(c: { id: number; type: string; title: string }) {
  if (c.id === ME) return { label: "پیوی شما", Icon: User };
  if (c.id === OTHER) return { label: "پیوی کاربر ۲", Icon: User };
  if (c.type === "channel") return { label: c.title, Icon: Megaphone };
  return { label: c.title, Icon: Users };
}

export default function TestChat({ workflow, onClose }: { workflow: () => Workflow; onClose: () => void }) {
  const [session, setSession] = useState<TestSession | null>(null);
  const [error, setError] = useState("");
  const [status, setStatus] = useState<LiveStatus>("connecting");
  const [chats, setChats] = useState<Record<number, ChatSnapshot>>({});
  const [events, setEvents] = useState<SimEvent[]>([]);
  const [active, setActive] = useState<number>(ME);
  const [sender, setSender] = useState<number>(ME);
  const [text, setText] = useState("");
  const [replyTo, setReplyTo] = useState<MsgView | null>(null);
  const [toast, setToast] = useState<{ text: string; alert: boolean } | null>(null);
  const [showLog, setShowLog] = useState(false);
  const [showMore, setShowMore] = useState(false);
  const [busy, setBusy] = useState(false);
  const listRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const sessionRef = useRef<TestSession | null>(null);

  const onLive = useCallback((m: LiveMessage) => {
    if (m.chat) setChats((c) => ({ ...c, [m.chat!.chat.id]: m.chat! }));
    const ev = m.event;
    if (ev && ev.seq > 0) {
      setEvents((list) => (list.some((e) => e.seq === ev.seq) ? list : [...list, ev].slice(-300)));
      const t = ev.text ?? "";
      if (ev.kind === "bot" && (t.startsWith("💬 toast") || t.startsWith("💬 alert"))) {
        setToast({ text: t.replace(/^💬 (toast|alert) to [^:]+: /, ""), alert: t.startsWith("💬 alert") });
      }
    }
  }, []);

  // Each start gets a generation; a session that finishes starting after
  // the panel moved on (closed, restarted) is stopped right away.
  const gen = useRef(0);
  const start = useCallback(async () => {
    const my = ++gen.current;
    setError("");
    setChats({});
    setEvents([]);
    setReplyTo(null);
    setSession(null);
    try {
      const s = await api.testStart(workflow());
      if (my !== gen.current) {
        api.testStop(s.id).catch(() => {});
        return;
      }
      sessionRef.current = s;
      setSession(s);
      const snaps = await Promise.all(s.chats.map((c) => api.testChat(s.id, c.id)));
      if (my === gen.current) setChats(Object.fromEntries(snaps.map((x) => [x.chat.id, x])));
    } catch (e) {
      if (my === gen.current) setError((e as Error).message);
    }
  }, [workflow]);

  useEffect(() => {
    start();
    return () => {
      gen.current++;
      if (sessionRef.current) api.testStop(sessionRef.current.id).catch(() => {});
      sessionRef.current = null;
    };
  }, [start]);

  useEffect(() => {
    if (!session) return;
    let stop: (() => void) | undefined;
    let cancelled = false;
    follow(session, onLive, setStatus).then((s) => (cancelled ? s() : (stop = s)));
    return () => {
      cancelled = true;
      stop?.();
    };
  }, [session, onLive]);

  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), toast.alert ? 6000 : 2800);
    return () => clearTimeout(t);
  }, [toast]);

  const snap = chats[active];
  useEffect(() => {
    listRef.current?.scrollTo({ top: listRef.current.scrollHeight, behavior: "smooth" });
  }, [snap?.messages.length, active]);

  const restart = async () => {
    if (sessionRef.current) await api.testStop(sessionRef.current.id).catch(() => {});
    sessionRef.current = null;
    start();
  };

  const effectiveSender = active === OTHER ? OTHER : active === ME ? ME : sender;
  const act = async (a: Record<string, unknown>) => {
    if (!session) return;
    setBusy(true);
    try {
      const r = await api.testAction(session.id, { chat_id: active, user_id: effectiveSender, ...a });
      setChats((c) => ({ ...c, [r.chat.chat.id]: r.chat }));
    } catch (e) {
      setToast({ text: (e as Error).message, alert: true });
    } finally {
      setBusy(false);
    }
  };

  const send = (t = text) => {
    if (!t.trim()) return;
    act({ type: "send", text: t, message_id: replyTo?.id ?? 0 });
    setText("");
    setReplyTo(null);
    inputRef.current?.focus();
  };

  const problems = events.filter((e) => e.kind === "error" || e.kind === "log").length;
  const isGroup = snap?.chat.type === "supergroup" || snap?.chat.type === "group";
  const isChannel = snap?.chat.type === "channel";
  const otherIsMember = useMemo(() => snap?.members?.some((m) => m.user_id === OTHER && !["left", "kicked"].includes(m.status)), [snap]);
  const byId = useMemo(() => Object.fromEntries((snap?.messages ?? []).map((m) => [m.id, m])), [snap]);

  return (
    <div className="tg">
      <div className="tg-head">
        <div className="tg-avatar">
          <Bot size={20} />
        </div>
        <div className="tg-who">
          <div className="tg-name">ربات شما · حالت تست</div>
          <div className={`tg-status st-${status}`}>{error ? "خطا در اجرا" : session ? statusText[status] : "در حال آماده‌سازی…"}</div>
        </div>
        <button type="button" className={`icon-btn${problems ? " badge" : ""}`} data-count={problems || undefined} title="رویدادها و لاگ" onClick={() => setShowLog(!showLog)}>
          <Activity size={16} />
        </button>
        <button type="button" className="icon-btn" title="اجرای دوباره با آخرین تغییرات" onClick={restart}>
          <RotateCcw size={16} />
        </button>
        <button type="button" className="icon-btn" title="بستن" onClick={onClose}>
          <X size={16} />
        </button>
      </div>

      {session && (
        <div className="tg-chats">
          {session.chats.map((c) => {
            const { label, Icon } = chatMeta(c);
            return (
              <button type="button" key={c.id} className={active === c.id ? "on" : ""} onClick={() => setActive(c.id)}>
                <Icon size={14} /> {label}
              </button>
            );
          })}
        </div>
      )}

      {isGroup && session && (
        <div className="tg-bar">
          <span>ارسال به‌عنوان</span>
          <div className="seg small">
            <button type="button" className={sender === ME ? "on" : ""} onClick={() => setSender(ME)}>
              شما (مالک)
            </button>
            <button type="button" className={sender === OTHER ? "on" : ""} onClick={() => setSender(OTHER)}>
              کاربر ۲ (عضو)
            </button>
          </div>
          {!otherIsMember && (
            <button type="button" className="btn btn-soft small" onClick={() => act({ type: "join", user_id: OTHER })}>
              <UserPlus size={14} /> ورود کاربر ۲
            </button>
          )}
        </div>
      )}

      <div className="tg-body">
        {error ? (
          <div className="tg-empty error">
            <p>{error}</p>
            <button type="button" className="btn btn-primary" onClick={start}>
              تلاش دوباره
            </button>
          </div>
        ) : !session ? (
          <div className="tg-empty">
            <span className="spinner" /> ربات در حال اجراست…
          </div>
        ) : (
          <div className="tg-list" ref={listRef}>
            {!snap?.messages.length && (
              <div className="tg-hint">
                {active === ME ? (
                  <>
                    پیامی نیست. با <button onClick={() => send("/start")}>/start</button> شروع کنید.
                  </>
                ) : isChannel ? (
                  "متنی بنویسید تا به‌عنوان پست کانال منتشر شود."
                ) : (
                  "پیامی نیست."
                )}
              </div>
            )}
            {snap?.messages.map((m) => (
              <Bubble key={m.id} m={m} group={!!isGroup} replied={m.reply_to ? byId[m.reply_to] : undefined} onPress={(b) => act({ type: "press", message_id: m.id, button: b })} onReply={() => (setReplyTo(m), inputRef.current?.focus())} />
            ))}
          </div>
        )}
        {toast && (
          <div className={`tg-toast${toast.alert ? " alert" : ""}`} onClick={() => setToast(null)}>
            {toast.text}
            {toast.alert && <button type="button">باشه</button>}
          </div>
        )}
        {showLog && (
          <div className="tg-log">
            <div className="tg-log-head">
              رویدادها
              <button type="button" className="icon-btn" onClick={() => setShowLog(false)}>
                <X size={14} />
              </button>
            </div>
            <div className="tg-log-list" dir="auto">
              {events.length === 0 && <div className="muted">هنوز رویدادی نیست.</div>}
              {events.slice(-120).map((e) => (
                <div key={e.seq} className={`log-line k-${e.kind}`}>
                  {e.chat && <span className="muted">{e.chat} · </span>}
                  {e.who && <b>{e.who}: </b>}
                  {e.text}
                </div>
              ))}
            </div>
          </div>
        )}
      </div>

      {snap?.keyboard && (
        <div className="tg-kb">
          {snap.keyboard.map((row, i) => (
            <div key={i} className="tg-kb-row">
              {row.map((b) => (
                <button type="button" key={b.text} onClick={() => (b.request_contact ? act({ type: "contact" }) : send(b.text))}>
                  {b.request_contact && <Phone size={13} />} {b.text}
                </button>
              ))}
            </div>
          ))}
        </div>
      )}

      {replyTo && (
        <div className="tg-replying">
          <Reply size={15} />
          <div>
            <b>{replyTo.from}</b>
            <span>{(replyTo.text || replyTo.caption || replyTo.info || "").slice(0, 60)}</span>
          </div>
          <button type="button" className="icon-btn" onClick={() => setReplyTo(null)}>
            <X size={14} />
          </button>
        </div>
      )}

      {session && (
        <form
          className="tg-input"
          onSubmit={(e) => {
            e.preventDefault();
            send();
          }}
        >
          <div className="tg-more-wrap">
            <button type="button" className="icon-btn" title="بیشتر" onClick={() => setShowMore(!showMore)}>
              <Plus size={18} />
            </button>
            {showMore && (
              <div className="pop tg-more" onMouseLeave={() => setShowMore(false)}>
                <button type="button" className="pop-item" disabled={snap?.chat.type !== "private"} onClick={() => (setShowMore(false), act({ type: "contact" }))}>
                  <Phone size={14} /> ارسال شماره تماس
                </button>
                <button
                  type="button"
                  className="pop-item"
                  onClick={() => {
                    setShowMore(false);
                    const q = prompt("متن inline query:", "go");
                    if (q !== null) act({ type: "inline", text: q });
                  }}
                >
                  <AtSign size={14} /> تست inline query
                </button>
              </div>
            )}
          </div>
          <input ref={inputRef} className="tg-text" dir="auto" value={text} placeholder={isChannel ? "متن پست کانال…" : "پیام…"} onChange={(e) => setText(e.target.value)} />
          <button type="submit" className="tg-send" disabled={!text.trim() || busy} aria-label="ارسال">
            <Send size={17} />
          </button>
        </form>
      )}
    </div>
  );
}

function Bubble({ m, group, replied, onPress, onReply }: { m: MsgView; group: boolean; replied?: MsgView; onPress: (b: string) => void; onReply: () => void }) {
  const body = m.text || m.caption || "";
  return (
    <div className={`msg ${m.by_bot ? "in" : "out"}${m.deleted ? " deleted" : ""}`}>
      <div className="bubble">
        {(group || m.by_bot) && <div className="b-from">{m.from}</div>}
        {replied && (
          <div className="b-quote">
            <b>{replied.from}</b>
            <span>{(replied.text || replied.caption || replied.info || "").slice(0, 50)}</span>
          </div>
        )}
        {m.media && <div className="b-media">{mediaLabel[m.media] ?? "📎 " + m.media}</div>}
        {m.info && <div className="b-info">{m.info}</div>}
        {body && (
          <div className="b-text" dir="auto">
            {body}
          </div>
        )}
        <div className="b-meta">
          {m.reactions?.length ? <span className="b-react">{m.reactions.join("")}</span> : null}
          {m.pinned && <span title="سنجاق شده">📌</span>}
          {m.deleted ? <span>حذف شد</span> : null}
          <span>#{m.id}</span>
          {!m.deleted && (
            <button type="button" className="b-reply" onClick={onReply} title="ریپلای">
              <CornerDownLeft size={12} />
            </button>
          )}
        </div>
      </div>
      {!m.deleted && m.buttons?.length ? (
        <div className="b-kb">
          {m.buttons.map((row, i) => (
            <div key={i} className="b-kb-row">
              {row.map((b) =>
                b.url ? (
                  <a key={b.text} href={b.url} target="_blank" rel="noreferrer">
                    {b.text} <Link2 size={11} />
                  </a>
                ) : (
                  <button type="button" key={b.text} onClick={() => onPress(b.text)}>
                    {b.text}
                  </button>
                ),
              )}
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}
