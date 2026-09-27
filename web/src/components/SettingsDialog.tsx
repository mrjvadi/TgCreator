import { Activity, Bot, Database, Plus, Trash2, Variable, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { WorkflowRest } from "../convert";
import type { Database as Db, Json } from "../types";
import { Switch } from "./Inspector";

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

const tabs: [Tab, string, typeof Bot][] = [
  ["bot", "ربات", Bot],
  ["runtime", "اجرا و کارایی", Activity],
  ["services", "Redis و دیتابیس", Database],
  ["vars", "متغیرها", Variable],
];

export default function SettingsDialog({ value, onChange, onClose }: { value: WorkflowRest; onChange: (v: WorkflowRest) => void; onClose: () => void }) {
  const [tab, setTab] = useState<Tab>("bot");
  const wf = value;
  const bot = wf.bot ?? { token: "" };
  const rt = wf.runtime ?? {};
  const svc = wf.services ?? {};
  const dbs = svc.databases ?? {};
  const setBot = (b: Partial<typeof bot>) => onChange({ ...wf, bot: { ...bot, ...b } });
  const setRt = (r: Partial<typeof rt>) => onChange({ ...wf, runtime: { ...rt, ...r } });
  const setDbs = (d: Record<string, Db>) => onChange({ ...wf, services: { ...svc, databases: Object.keys(d).length ? d : undefined } });
  const [vars, setVars] = useState<[string, string][]>(() => Object.entries(wf.variables ?? {}).map(([k, v]) => [k, typeof v === "string" ? v : JSON.stringify(v)]));
  const commitVars = (rows: [string, string][]) => {
    setVars(rows);
    const out: Record<string, Json> = {};
    for (const [k, v] of rows) if (k.trim()) out[k.trim()] = parseValue(v);
    onChange({ ...wf, variables: out });
  };
  const num = (v: string) => (v === "" ? undefined : Number(v));
  // Registered once (capture phase): re-registering on every render would
  // drop the listener while the editor's own Escape handler re-renders.
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      e.stopPropagation();
      closeRef.current();
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, []);

  return (
    <div className="modal-back" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="modal settings" role="dialog" aria-label="تنظیمات workflow">
        <nav className="settings-nav">
          <div className="settings-title">تنظیمات</div>
          {tabs.map(([k, l, I]) => (
            <button type="button" key={k} className={tab === k ? "on" : ""} onClick={() => setTab(k)}>
              <I size={16} /> {l}
            </button>
          ))}
        </nav>
        <div className="settings-main">
          <div className="settings-head">
            <h2>{tabs.find((t) => t[0] === tab)?.[1]}</h2>
            <button type="button" className="icon-btn" onClick={onClose} aria-label="بستن">
              <X size={16} />
            </button>
          </div>
          <div className="settings-body">
            {tab === "bot" && (
              <>
                <Row label="نام workflow">
                  <input className="input" dir="auto" value={wf.name} onChange={(e) => onChange({ ...wf, name: e.target.value })} />
                </Row>
                <Row label="توکن ربات" help="بهتر است توکن داخل فایل نباشد؛ ${BOT_TOKEN} از متغیر محیطی خوانده می‌شود.">
                  <input className="input mono" dir="ltr" value={bot.token} onChange={(e) => setBot({ token: e.target.value })} />
                </Row>
                <div className="field">
                  <span className="field-label">دریافت آپدیت‌ها</span>
                  <div className="seg wide">
                    <button type="button" className={(bot.mode ?? "polling") === "polling" ? "on" : ""} onClick={() => setBot({ mode: "polling" })}>
                      Long polling
                    </button>
                    <button type="button" className={bot.mode === "webhook" ? "on" : ""} onClick={() => setBot({ mode: "webhook" })}>
                      Webhook
                    </button>
                  </div>
                </div>
                {bot.mode === "webhook" && (
                  <div className="card-soft">
                    <Row label="آدرس عمومی webhook">
                      <input className="input mono" dir="ltr" placeholder="https://bot.example.com/webhook" value={bot.webhook?.url ?? ""} onChange={(e) => setBot({ webhook: { ...bot.webhook, url: e.target.value } })} />
                    </Row>
                    <Row label="آدرس گوش دادن">
                      <input className="input mono" dir="ltr" placeholder=":8080" value={bot.webhook?.listen ?? ""} onChange={(e) => setBot({ webhook: { ...bot.webhook, listen: e.target.value } })} />
                    </Row>
                    <Row label="Secret token">
                      <input className="input mono" dir="ltr" value={bot.webhook?.secret_token ?? ""} onChange={(e) => setBot({ webhook: { ...bot.webhook, secret_token: e.target.value } })} />
                    </Row>
                  </div>
                )}
                <Row label="قالب پیش‌فرض متن پیام‌ها">
                  <select className="input" value={bot.parse_mode ?? ""} onChange={(e) => setBot({ parse_mode: e.target.value || undefined })}>
                    <option value="">بدون قالب</option>
                    <option value="HTML">HTML</option>
                    <option value="MarkdownV2">MarkdownV2</option>
                  </select>
                </Row>
                <Row label="سرور Bot API شخصی" help="اختیاری؛ برای Local Bot API Server">
                  <input className="input mono" dir="ltr" placeholder="https://api.telegram.org" value={bot.api_url ?? ""} onChange={(e) => setBot({ api_url: e.target.value || undefined })} />
                </Row>
              </>
            )}
            {tab === "runtime" && (
              <>
                <div className="grid2">
                  <Row label="پردازش هم‌زمان" help="پیش‌فرض ۵۱۲">
                    <input className="input" type="number" dir="ltr" value={rt.workers ?? ""} placeholder="512" onChange={(e) => setRt({ workers: num(e.target.value) })} />
                  </Row>
                  <Row label="حداکثر صف" help="پیش‌فرض ۱۰۰٬۰۰۰">
                    <input className="input" type="number" dir="ltr" value={rt.queue_size ?? ""} placeholder="100000" onChange={(e) => setRt({ queue_size: num(e.target.value) })} />
                  </Row>
                  <Row label="ذخیرهٔ وضعیت کاربر">
                    <select className="input" value={rt.state_backend ?? "memory"} onChange={(e) => setRt({ state_backend: e.target.value })}>
                      <option value="memory">حافظه</option>
                      <option value="redis">Redis</option>
                    </select>
                  </Row>
                  <Row label="انقضای وضعیت">
                    <input className="input mono" dir="ltr" placeholder="24h" value={rt.state_ttl ?? ""} onChange={(e) => setRt({ state_ttl: e.target.value || undefined })} />
                  </Row>
                  <Row label="Health check" help="GET /healthz">
                    <input className="input mono" dir="ltr" placeholder=":9090" value={rt.health_listen ?? ""} onChange={(e) => setRt({ health_listen: e.target.value || undefined })} />
                  </Row>
                  <Row label="حداکثر گام هر اجرا">
                    <input className="input" type="number" dir="ltr" placeholder="1000" value={rt.max_steps ?? ""} onChange={(e) => setRt({ max_steps: num(e.target.value) })} />
                  </Row>
                </div>
                <Switch checked={!!rt.drop_pending} onChange={(v) => setRt({ drop_pending: v || undefined })} label="آپدیت‌های قدیمی هنگام شروع نادیده گرفته شوند" />
              </>
            )}
            {tab === "services" && (
              <>
                <Row label="آدرس Redis" help="فقط وقتی نودی از Redis استفاده کند وصل می‌شود">
                  <input
                    className="input mono"
                    dir="ltr"
                    placeholder="${REDIS_URL:-redis://localhost:6379/0}"
                    value={svc.redis?.url ?? ""}
                    onChange={(e) => onChange({ ...wf, services: { ...svc, redis: e.target.value ? { url: e.target.value } : undefined } })}
                  />
                </Row>
                <div className="section-title">دیتابیس‌ها</div>
                {Object.entries(dbs).map(([name, db]) => (
                  <div key={name} className="card-soft">
                    <div className="db-head">
                      <Database size={15} />
                      <code dir="ltr">{name}</code>
                      <div className="seg">
                        {[
                          ["postgres", "Postgres"],
                          ["mysql", "MySQL"],
                          ["sqlite", "SQLite"],
                        ].map(([k, l]) => (
                          <button type="button" key={k} className={db.driver === k ? "on" : ""} onClick={() => setDbs({ ...dbs, [name]: { ...db, driver: k } })}>
                            {l}
                          </button>
                        ))}
                      </div>
                      <button
                        type="button"
                        className="icon-btn danger"
                        onClick={() => {
                          const next = { ...dbs };
                          delete next[name];
                          setDbs(next);
                        }}
                        aria-label="حذف"
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                    <Row label="DSN">
                      <input className="input mono" dir="ltr" value={db.dsn} placeholder="${DATABASE_URL}" onChange={(e) => setDbs({ ...dbs, [name]: { ...db, dsn: e.target.value } })} />
                    </Row>
                    <Row label="Migrationها" help="هر دستور در یک خط؛ هنگام شروع اجرا می‌شوند">
                      <textarea
                        className="input mono code"
                        dir="ltr"
                        rows={4}
                        value={(db.migrations ?? []).join("\n")}
                        onChange={(e) => setDbs({ ...dbs, [name]: { ...db, migrations: e.target.value.split("\n").filter((l) => l.trim()) } })}
                      />
                    </Row>
                  </div>
                ))}
                <button
                  type="button"
                  className="add-line"
                  onClick={() => {
                    let n = Object.keys(dbs).length ? `db${Object.keys(dbs).length + 1}` : "main";
                    while (dbs[n]) n += "_";
                    setDbs({ ...dbs, [n]: { driver: "postgres", dsn: "${DATABASE_URL}", migrations: [] } });
                  }}
                >
                  <Plus size={14} /> افزودن دیتابیس
                </button>
              </>
            )}
            {tab === "vars" && (
              <>
                <p className="muted">در نودها با <code dir="ltr">vars.name</code> در دسترس‌اند. عدد، true/false و JSON خودکار تشخیص داده می‌شوند.</p>
                <div className="kv">
                  {vars.map(([k, v], i) => (
                    <div key={i} className="kv-row">
                      <input className="input mono kv-key" dir="ltr" placeholder="name" value={k} onChange={(e) => commitVars(vars.map((r, j) => (j === i ? [e.target.value, r[1]] : r)))} />
                      <input className="input" dir="auto" placeholder="مقدار" value={v} onChange={(e) => commitVars(vars.map((r, j) => (j === i ? [r[0], e.target.value] : r)))} />
                      <button type="button" className="icon-btn danger" onClick={() => commitVars(vars.filter((_, j) => j !== i))} aria-label="حذف">
                        <Trash2 size={14} />
                      </button>
                    </div>
                  ))}
                  <button type="button" className="add-line" onClick={() => setVars([...vars, ["", ""]])}>
                    <Plus size={14} /> افزودن متغیر
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
