import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";
import { follow, type LiveMessage, type LiveStatus } from "../realtime";
import type { ChatSnapshot, MsgView, SimEvent, TestSession, Workflow } from "../types";

const ME = 1001;
const OTHER = 1002;

const statusText: Record<LiveStatus, string> = {
  connecting: "در حال اتصال به Centrifugo…",
  live: "زنده (Centrifugo)",
  polling: "بدون Centrifugo (polling)",
  offline: "قطع",
};

const mediaIcon: Record<string, string> = {
  photo: "📷", video: "🎬", audio: "🎵", document: "📄", animation: "🎞", voice: "🎤", video_note: "⏺", sticker: "🏷",
  poll: "📊", dice: "🎲", invoice: "🧾", contact: "👤", location: "📍",
};

function chatTitle(c: { id: number; type: string; title: string }) {
  if (c.id === ME) return "پیوی شما";
  if (c.id === OTHER) return "پیوی کاربر تست";
  return c.type === "channel" ? `📢 ${c.title}` : `👥 ${c.title}`;
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
  const [busy, setBusy] = useState(false);
  const listRef = useRef<HTMLDivElement>(null);
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
    const t = setTimeout(() => setToast(null), toast.alert ? 6000 : 3000);
    return () => clearTimeout(t);
  }, [toast]);

  const snap = chats[active];
  useEffect(() => {
    listRef.current?.scrollTo({ top: listRef.current.scrollHeight });
  }, [snap?.messages.length, active]);

  const restart = async () => {
    if (sessionRef.current) await api.testStop(sessionRef.current.id).catch(() => {});
    sessionRef.current = null;
    setSession(null);
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
  };

  const problems = events.filter((e) => e.kind === "error" || e.kind === "log");
  const isGroup = snap?.chat.type === "supergroup" || snap?.chat.type === "group";
  const otherIsMember = useMemo(() => snap?.members?.some((m) => m.user_id === OTHER && !["left", "kicked"].includes(m.status)), [snap]);

  return (
    <div className="testchat">
      <div className="tc-head">
        <div>
          <div className="tc-title">تست زنده</div>
          <div className={`tc-status st-${status}`}>● {statusText[status]}</div>
        </div>
        <div className="tc-head-actions">
          <button type="button" className="btn-ghost small" onClick={restart} title="اجرای دوباره با آخرین تغییرات">
            ↻ از نو
          </button>
          <button type="button" className="btn-icon" onClick={onClose} aria-label="بستن">
            ×
          </button>
        </div>
      </div>

      {error ? (
        <div className="tc-error">
          <p>{error}</p>
          <button type="button" className="btn" onClick={start}>
            تلاش دوباره
          </button>
        </div>
      ) : !session ? (
        <div className="tc-empty">در حال آماده‌سازی ربات…</div>
      ) : (
        <>
          <div className="tc-tabs">
            {session.chats.map((c) => (
              <button type="button" key={c.id} className={`tc-tab${active === c.id ? " active" : ""}`} onClick={() => setActive(c.id)}>
                {chatTitle(c)}
              </button>
            ))}
          </div>

          {isGroup && (
            <div className="tc-bar">
              <label>
                ارسال به‌عنوان:
                <select className="input small" value={sender} onChange={(e) => setSender(Number(e.target.value))}>
                  {session.users.map((u) => (
                    <option key={u.id} value={u.id}>
                      {u.name}
                    </option>
                  ))}
                </select>
              </label>
              {!otherIsMember && (
                <button type="button" className="btn-ghost small" onClick={() => act({ type: "join", user_id: OTHER })}>
                  ➕ ورود کاربر تست
                </button>
              )}
            </div>
          )}

          <div className="tc-list" ref={listRef}>
            {snap?.messages.length ? null : <div className="tc-empty">پیامی نیست. {active === ME ? "مثلاً /start بفرستید." : ""}</div>}
            {snap?.messages.map((m) => (
              <Bubble key={m.id} m={m} group={isGroup} onPress={(b) => act({ type: "press", message_id: m.id, button: b })} onReply={() => setReplyTo(m)} />
            ))}
          </div>

          {toast && <div className={`tc-toast${toast.alert ? " alert" : ""}`} onClick={() => setToast(null)}>{toast.text}</div>}

          {snap?.keyboard && (
            <div className="tc-kb">
              {snap.keyboard.map((row, i) => (
                <div key={i} className="tc-kb-row">
                  {row.map((b) => (
                    <button type="button" key={b.text} className="tc-kb-btn" onClick={() => (b.request_contact ? act({ type: "contact" }) : send(b.text))}>
                      {b.text}
                    </button>
                  ))}
                </div>
              ))}
            </div>
          )}

          {replyTo && (
            <div className="tc-replying">
              ↩️ در پاسخ به: {(replyTo.text || replyTo.caption || replyTo.media || "").slice(0, 40)}
              <button type="button" className="btn-icon" onClick={() => setReplyTo(null)}>
                ×
              </button>
            </div>
          )}
          <form
            className="tc-input"
            onSubmit={(e) => {
              e.preventDefault();
              send();
            }}
          >
            <input className="input" dir="auto" value={text} placeholder={snap?.chat.type === "channel" ? "متن پست کانال…" : "پیام…"} onChange={(e) => setText(e.target.value)} />
            <button type="submit" className="btn" disabled={!text.trim() || busy}>
              ارسال
            </button>
          </form>
          <div className="tc-more">
            <button type="button" className="btn-ghost small" onClick={() => act({ type: "contact" })} disabled={snap?.chat.type !== "private"}>
              📱 ارسال شماره
            </button>
            <button
              type="button"
              className="btn-ghost small"
              onClick={() => {
                const q = prompt("متن inline query:", "go");
                if (q !== null) act({ type: "inline", text: q });
              }}
            >
              ⌨️ inline
            </button>
            <button type="button" className={`btn-ghost small${problems.length ? " warn" : ""}`} onClick={() => setShowLog(!showLog)}>
              رویدادها {problems.length ? `(${problems.length} خطا)` : ""}
            </button>
          </div>
          {showLog && (
            <div className="tc-log" dir="auto">
              {events.slice(-80).map((e) => (
                <div key={e.seq} className={`tc-log-line k-${e.kind}`}>
                  {e.chat && <span className="muted">[{e.chat}] </span>}
                  {e.who && <b>{e.who}: </b>}
                  {e.text}
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}

function Bubble({ m, group, onPress, onReply }: { m: MsgView; group: boolean; onPress: (b: string) => void; onReply: () => void }) {
  const body = m.text || m.caption || "";
  return (
    <div className={`bubble ${m.by_bot ? "bot" : "user"}${m.deleted ? " deleted" : ""}`}>
      {(group || m.by_bot) && <div className="b-from">{m.from}</div>}
      {m.media && (
        <div className="b-media">
          {mediaIcon[m.media] ?? "📎"} {m.media}
        </div>
      )}
      {m.info && <div className="b-info">{m.info}</div>}
      {body && (
        <div className="b-text" dir="auto">
          {body}
        </div>
      )}
      <div className="b-meta">
        {m.pinned && "📌 "}
        {m.reactions?.join("")} {m.deleted ? "حذف شد" : ""} #{m.id}
        {!m.deleted && (
          <button type="button" className="b-reply" onClick={onReply} title="ریپلای">
            ↩
          </button>
        )}
      </div>
      {!m.deleted &&
        m.buttons?.map((row, i) => (
          <div key={i} className="b-btns">
            {row.map((b) =>
              b.url ? (
                <a key={b.text} className="b-btn" href={b.url} target="_blank" rel="noreferrer">
                  {b.text} ↗
                </a>
              ) : (
                <button type="button" key={b.text} className="b-btn" onClick={() => onPress(b.text)}>
                  {b.text}
                </button>
              ),
            )}
          </div>
        ))}
    </div>
  );
}
