import { useEffect, useState } from "react";
import type { WorkflowRest } from "../convert";
import type { Database, Json } from "../types";

type Tab = "bot" | "runtime" | "services" | "vars";

function parseValue(s: string): Json {
  const t = s.trim();
  if (t === "true" || t === "false") return t === "true";
  if (/^-?\d+(\.\d+)?$/.test(t)) return Number(t);
  if ((t.startsWith("{") && t.endsWith("}")) || (t.startsWith("[") && t.endsWith("]"))) {
    try {
      return JSON.parse(t);
    } catch {
      /* keep as text */
    }
  }
  return s;
}

function Row({ label, help, children }: { label: string; help?: string; children: React.ReactNode }) {
  return (
    <label className="field">
      <span className="field-label">{label}</span>
      {children}
      {help && <span className="field-help">{help}</span>}
    </label>
  );
}

export default function SettingsDialog({ value, onChange, onClose }: { value: WorkflowRest; onChange: (v: WorkflowRest) => void; onClose: () => void }) {
  const [tab, setTab] = useState<Tab>("bot");
  const wf = value;
  const bot = wf.bot ?? { token: "" };
  const rt = wf.runtime ?? {};
  const svc = wf.services ?? {};
  const dbs = svc.databases ?? {};
  const setBot = (b: Partial<typeof bot>) => onChange({ ...wf, bot: { ...bot, ...b } });
  const setRt = (r: Partial<typeof rt>) => onChange({ ...wf, runtime: { ...rt, ...r } });
  const setDbs = (d: Record<string, Database>) => onChange({ ...wf, services: { ...svc, databases: Object.keys(d).length ? d : undefined } });
  const [vars, setVars] = useState<[string, string][]>(() =>
    Object.entries(wf.variables ?? {}).map(([k, v]) => [k, typeof v === "string" ? v : JSON.stringify(v)]),
  );
  const commitVars = (rows: [string, string][]) => {
    setVars(rows);
    const out: Record<string, Json> = {};
    for (const [k, v] of rows) if (k.trim()) out[k.trim()] = parseValue(v);
    onChange({ ...wf, variables: out });
  };
  const num = (v: string) => (v === "" ? undefined : Number(v));
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div className="modal-back" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="modal" role="dialog" aria-label="تنظیمات">
        <div className="modal-head">
          <h2>تنظیمات workflow</h2>
          <button type="button" className="btn-icon" onClick={onClose} aria-label="بستن">
            ×
          </button>
        </div>
        <div className="tabs">
          {(
            [
              ["bot", "ربات"],
              ["runtime", "اجرا"],
              ["services", "سرویس‌ها"],
              ["vars", "متغیرها"],
            ] as [Tab, string][]
          ).map(([k, l]) => (
            <button type="button" key={k} className={`tab${tab === k ? " active" : ""}`} onClick={() => setTab(k)}>
              {l}
            </button>
          ))}
        </div>
        <div className="modal-body">
          {tab === "bot" && (
            <>
              <Row label="نام workflow">
                <input className="input" dir="auto" value={wf.name} onChange={(e) => onChange({ ...wf, name: e.target.value })} />
              </Row>
              <Row label="توکن ربات" help="بهتر است توکن در فایل نباشد: ${BOT_TOKEN} از متغیر محیطی خوانده می‌شود">
                <input className="input mono" dir="ltr" value={bot.token} onChange={(e) => setBot({ token: e.target.value })} />
              </Row>
              <Row label="حالت دریافت آپدیت">
                <select className="input" value={bot.mode ?? "polling"} onChange={(e) => setBot({ mode: e.target.value })}>
                  <option value="polling">Long polling</option>
                  <option value="webhook">Webhook</option>
                </select>
              </Row>
              {bot.mode === "webhook" && (
                <>
                  <Row label="آدرس عمومی webhook">
                    <input className="input mono" dir="ltr" placeholder="https://bot.example.com/webhook" value={bot.webhook?.url ?? ""} onChange={(e) => setBot({ webhook: { ...bot.webhook, url: e.target.value } })} />
                  </Row>
                  <Row label="آدرس گوش دادن">
                    <input className="input mono" dir="ltr" placeholder=":8080" value={bot.webhook?.listen ?? ""} onChange={(e) => setBot({ webhook: { ...bot.webhook, listen: e.target.value } })} />
                  </Row>
                  <Row label="Secret token">
                    <input className="input mono" dir="ltr" value={bot.webhook?.secret_token ?? ""} onChange={(e) => setBot({ webhook: { ...bot.webhook, secret_token: e.target.value } })} />
                  </Row>
                </>
              )}
              <Row label="قالب پیش‌فرض متن">
                <select className="input" value={bot.parse_mode ?? ""} onChange={(e) => setBot({ parse_mode: e.target.value || undefined })}>
                  <option value="">بدون قالب</option>
                  <option value="HTML">HTML</option>
                  <option value="MarkdownV2">MarkdownV2</option>
                </select>
              </Row>
              <Row label="Bot API سرور شخصی (اختیاری)">
                <input className="input mono" dir="ltr" placeholder="https://api.telegram.org" value={bot.api_url ?? ""} onChange={(e) => setBot({ api_url: e.target.value || undefined })} />
              </Row>
            </>
          )}
          {tab === "runtime" && (
            <>
              <Row label="پردازش هم‌زمان" help="پیش‌فرض ۵۱۲؛ هر چت صف خودش را دارد">
                <input className="input" type="number" dir="ltr" value={rt.workers ?? ""} placeholder="512" onChange={(e) => setRt({ workers: num(e.target.value) })} />
              </Row>
              <Row label="حداکثر صف" help="اگر پر شود دریافت آپدیت موقتاً متوقف می‌شود">
                <input className="input" type="number" dir="ltr" value={rt.queue_size ?? ""} placeholder="100000" onChange={(e) => setRt({ queue_size: num(e.target.value) })} />
              </Row>
              <Row label="ذخیرهٔ وضعیت کاربر">
                <select className="input" value={rt.state_backend ?? "memory"} onChange={(e) => setRt({ state_backend: e.target.value })}>
                  <option value="memory">حافظه (با ری‌استارت پاک می‌شود)</option>
                  <option value="redis">Redis</option>
                </select>
              </Row>
              <Row label="انقضای وضعیت کاربر">
                <input className="input mono" dir="ltr" placeholder="24h" value={rt.state_ttl ?? ""} onChange={(e) => setRt({ state_ttl: e.target.value || undefined })} />
              </Row>
              <Row label="Health check" help="مثلاً :9090 ← GET /healthz">
                <input className="input mono" dir="ltr" value={rt.health_listen ?? ""} onChange={(e) => setRt({ health_listen: e.target.value || undefined })} />
              </Row>
              <Row label="حداکثر گام در هر اجرا">
                <input className="input" type="number" dir="ltr" placeholder="1000" value={rt.max_steps ?? ""} onChange={(e) => setRt({ max_steps: num(e.target.value) })} />
              </Row>
              <label className="check">
                <input type="checkbox" checked={!!rt.drop_pending} onChange={(e) => setRt({ drop_pending: e.target.checked || undefined })} />
                آپدیت‌های قدیمی هنگام شروع نادیده گرفته شوند
              </label>
            </>
          )}
          {tab === "services" && (
            <>
              <Row label="آدرس Redis" help="فقط اگر نودی از Redis استفاده کند وصل می‌شود">
                <input
                  className="input mono"
                  dir="ltr"
                  placeholder="${REDIS_URL:-redis://localhost:6379/0}"
                  value={svc.redis?.url ?? ""}
                  onChange={(e) => onChange({ ...wf, services: { ...svc, redis: e.target.value ? { url: e.target.value } : undefined } })}
                />
              </Row>
              <div className="sec-title">دیتابیس‌ها</div>
              {Object.entries(dbs).map(([name, db]) => (
                <div key={name} className="db-card">
                  <div className="field-row">
                    <code dir="ltr">{name}</code>
                    <select className="input small" value={db.driver} onChange={(e) => setDbs({ ...dbs, [name]: { ...db, driver: e.target.value } })}>
                      <option value="postgres">PostgreSQL</option>
                      <option value="mysql">MySQL</option>
                      <option value="sqlite">SQLite</option>
                    </select>
                    <button
                      type="button"
                      className="btn-icon danger"
                      onClick={() => {
                        const next = { ...dbs };
                        delete next[name];
                        setDbs(next);
                      }}
                    >
                      ×
                    </button>
                  </div>
                  <Row label="DSN">
                    <input className="input mono" dir="ltr" value={db.dsn} placeholder="${DATABASE_URL}" onChange={(e) => setDbs({ ...dbs, [name]: { ...db, dsn: e.target.value } })} />
                  </Row>
                  <Row label="Migration ها" help="هر دستور در یک خط؛ هنگام شروع اجرا می‌شوند">
                    <textarea
                      className="input mono"
                      dir="ltr"
                      rows={3}
                      value={(db.migrations ?? []).join("\n")}
                      onChange={(e) => setDbs({ ...dbs, [name]: { ...db, migrations: e.target.value.split("\n").filter((l) => l.trim()) } })}
                    />
                  </Row>
                </div>
              ))}
              <button
                type="button"
                className="btn-ghost small"
                onClick={() => {
                  let n = Object.keys(dbs).length ? `db${Object.keys(dbs).length + 1}` : "main";
                  while (dbs[n]) n += "_";
                  setDbs({ ...dbs, [n]: { driver: "postgres", dsn: "${DATABASE_URL}", migrations: [] } });
                }}
              >
                + دیتابیس
              </button>
            </>
          )}
          {tab === "vars" && (
            <>
              <p className="muted">در نودها با vars.نام در دسترس‌اند. عدد، true/false و JSON خودکار تشخیص داده می‌شوند.</p>
              {vars.map(([k, v], i) => (
                <div key={i} className="field-row">
                  <input className="input mono key" dir="ltr" placeholder="نام" value={k} onChange={(e) => commitVars(vars.map((r, j) => (j === i ? [e.target.value, r[1]] : r)))} />
                  <input className="input" dir="auto" placeholder="مقدار" value={v} onChange={(e) => commitVars(vars.map((r, j) => (j === i ? [r[0], e.target.value] : r)))} />
                  <button type="button" className="btn-icon danger" onClick={() => commitVars(vars.filter((_, j) => j !== i))}>
                    ×
                  </button>
                </div>
              ))}
              <button type="button" className="btn-ghost small" onClick={() => setVars([...vars, ["", ""]])}>
                + متغیر
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
