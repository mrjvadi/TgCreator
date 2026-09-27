import { useContext, useEffect, useId, useRef, useState } from "react";
import { CatalogContext } from "../context";
import type { Json } from "../types";

// ---------- variable picker ----------

export interface VarGroup {
  title: string;
  items: { expr: string; label: string }[];
}

export const baseVars: VarGroup[] = [
  {
    title: "پیام و کاربر",
    items: [
      { expr: "text", label: "متن پیام" },
      { expr: "from.first_name", label: "نام کاربر" },
      { expr: "from.id", label: "شناسهٔ کاربر" },
      { expr: "from.username", label: "یوزرنیم" },
      { expr: "mention(from)", label: "منشن کاربر (HTML)" },
      { expr: "chat.id", label: "شناسهٔ چت" },
      { expr: "chat.title", label: "نام گروه" },
      { expr: "chat.type", label: "نوع چت" },
      { expr: "message.message_id", label: "شناسهٔ پیام" },
      { expr: "command", label: "دستور" },
      { expr: "args[0]", label: "آرگومان اول" },
      { expr: "args_text", label: "همهٔ آرگومان‌ها" },
      { expr: "data", label: "callback_data دکمه" },
      { expr: "reply.from.id", label: "کاربرِ پیام ریپلای‌شده" },
      { expr: "bot.username", label: "یوزرنیم ربات" },
    ],
  },
  {
    title: "توابع",
    items: [
      { expr: "escapeHTML(text)", label: "متن امن برای HTML" },
      { expr: "hasLink(message)", label: "لینک دارد؟" },
      { expr: "upper(text)", label: "حروف بزرگ" },
      { expr: "now().Unix()", label: "زمان فعلی (ثانیه)" },
      { expr: "len(text)", label: "طول متن" },
    ],
  },
];

export function VarMenu({ groups, onPick }: { groups: VarGroup[]; onPick: (expr: string) => void }) {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => !ref.current?.contains(e.target as globalThis.Node) && setOpen(false);
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [open]);
  const match = (s: string) => !q || s.toLowerCase().includes(q.toLowerCase());
  return (
    <div className="varmenu" ref={ref}>
      <button type="button" className="btn-icon" title="درج متغیر" onClick={() => setOpen(!open)}>
        {"{x}"}
      </button>
      {open && (
        <div className="varmenu-pop">
          <input autoFocus className="input" placeholder="جست‌وجو…" value={q} onChange={(e) => setQ(e.target.value)} />
          <div className="varmenu-list">
            {groups.map((g) => {
              const items = g.items.filter((i) => match(i.expr) || match(i.label));
              if (!items.length) return null;
              return (
                <div key={g.title}>
                  <div className="varmenu-title">{g.title}</div>
                  {items.map((i) => (
                    <button
                      type="button"
                      key={i.expr}
                      className="varmenu-item"
                      onClick={() => {
                        onPick(i.expr);
                        setOpen(false);
                      }}
                    >
                      <code dir="ltr">{i.expr}</code>
                      <span>{i.label}</span>
                    </button>
                  ))}
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}

function insertAt(el: HTMLInputElement | HTMLTextAreaElement | null, value: string, snippet: string): string {
  if (!el || el.selectionStart === null) return value + snippet;
  const s = el.selectionStart;
  const e = el.selectionEnd ?? s;
  return value.slice(0, s) + snippet + value.slice(e);
}

// ---------- text-like fields ----------

export function TextField(props: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  multiline?: boolean;
  mono?: boolean;
  bare?: boolean; // expressions: insert without {{ }}
  vars?: VarGroup[];
  list?: string[];
}) {
  const ref = useRef<HTMLInputElement & HTMLTextAreaElement>(null);
  const listId = useId();
  const pick = (expr: string) => props.onChange(insertAt(ref.current, props.value, props.bare ? expr : `{{ ${expr} }}`));
  const common = {
    ref,
    className: `input${props.mono ? " mono" : ""}`,
    value: props.value,
    placeholder: props.placeholder,
    dir: props.mono ? "ltr" : "auto",
    onChange: (e: React.ChangeEvent<HTMLInputElement & HTMLTextAreaElement>) => props.onChange(e.target.value),
  };
  return (
    <div className="field-row">
      {props.multiline ? (
        <textarea {...common} rows={props.mono ? 3 : 4} spellCheck={!props.mono} />
      ) : (
        <input {...common} list={props.list ? listId : undefined} />
      )}
      {props.list && (
        <datalist id={listId}>
          {props.list.map((o) => (
            <option key={o} value={o} />
          ))}
        </datalist>
      )}
      {props.vars && <VarMenu groups={props.vars} onPick={pick} />}
    </div>
  );
}

// ---------- tags ----------

export function TagsInput({ value, onChange, options, placeholder }: { value: string[]; onChange: (v: string[]) => void; options?: string[]; placeholder?: string }) {
  const [draft, setDraft] = useState("");
  const listId = useId();
  const add = (t: string) => {
    const v = t.trim();
    if (v && !value.includes(v)) onChange([...value, v]);
    setDraft("");
  };
  return (
    <div className="tags">
      {value.map((t) => (
        <span key={t} className="tag" dir="auto">
          {t}
          <button type="button" onClick={() => onChange(value.filter((x) => x !== t))} aria-label="حذف">
            ×
          </button>
        </span>
      ))}
      <input
        className="tags-input"
        value={draft}
        list={options ? listId : undefined}
        placeholder={value.length ? "" : placeholder ?? "بنویس و Enter"}
        onChange={(e) => {
          const v = e.target.value;
          if (options?.includes(v)) add(v);
          else setDraft(v);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === ",") {
            e.preventDefault();
            add(draft);
          } else if (e.key === "Backspace" && !draft && value.length) {
            onChange(value.slice(0, -1));
          }
        }}
        onBlur={() => draft && add(draft)}
      />
      {options && (
        <datalist id={listId}>
          {options.filter((o) => !value.includes(o)).map((o) => (
            <option key={o} value={o} />
          ))}
        </datalist>
      )}
    </div>
  );
}

// ---------- inline keyboard ----------

type Btn = Record<string, Json>;
const btnKinds: [string, string][] = [
  ["callback_data", "دکمهٔ عملیات"],
  ["url", "لینک"],
  ["web_app", "مینی‌اپ"],
  ["switch_inline_query_current_chat", "جست‌وجوی inline"],
];

function rowsOf(v: Json | undefined): Btn[][] {
  if (!Array.isArray(v)) return [];
  return v.map((r) => (Array.isArray(r) ? (r as Btn[]) : [r as Btn]));
}

function kindOf(b: Btn): string {
  return btnKinds.find(([k]) => k in b)?.[0] ?? "callback_data";
}

export function ButtonsEditor({ value, onChange, vars }: { value: Json | undefined; onChange: (v: Json) => void; vars: VarGroup[] }) {
  const rows = rowsOf(value);
  const set = (r: Btn[][]) => onChange(r.filter((row) => row.length) as Json);
  const update = (ri: number, bi: number, b: Btn) => set(rows.map((row, i) => (i === ri ? row.map((x, j) => (j === bi ? b : x)) : row)));
  return (
    <div className="kb">
      {rows.map((row, ri) => (
        <div key={ri} className="kb-row">
          {row.map((b, bi) => {
            const kind = kindOf(b);
            const raw = b[kind];
            const val = kind === "web_app" ? String((raw as { url?: string })?.url ?? "") : String(raw ?? "");
            return (
              <div key={bi} className="kb-btn">
                <div className="field-row">
                  <input className="input" dir="auto" placeholder="متن دکمه" value={String(b.text ?? "")} onChange={(e) => update(ri, bi, { ...b, text: e.target.value })} />
                  <button type="button" className="btn-icon danger" title="حذف دکمه" onClick={() => set(rows.map((r, i) => (i === ri ? r.filter((_, j) => j !== bi) : r)))}>
                    ×
                  </button>
                </div>
                <div className="field-row">
                  <select
                    className="input small"
                    value={kind}
                    onChange={(e) => {
                      const nb: Btn = { text: b.text ?? "" };
                      nb[e.target.value] = e.target.value === "web_app" ? { url: val } : val;
                      update(ri, bi, nb);
                    }}
                  >
                    {btnKinds.map(([k, l]) => (
                      <option key={k} value={k}>
                        {l}
                      </option>
                    ))}
                  </select>
                  <TextField
                    value={val}
                    placeholder={kind === "callback_data" ? "مثلاً buy:1" : "https://…"}
                    onChange={(v) => update(ri, bi, { ...b, [kind]: kind === "web_app" ? { url: v } : v })}
                    vars={vars}
                  />
                </div>
              </div>
            );
          })}
          <button type="button" className="btn-ghost small" onClick={() => set(rows.map((r, i) => (i === ri ? [...r, { text: "", callback_data: "" }] : r)))}>
            + دکمه در این ردیف
          </button>
        </div>
      ))}
      <button type="button" className="btn-ghost small" onClick={() => set([...rows, [{ text: "", callback_data: "" }]])}>
        + ردیف جدید
      </button>
    </div>
  );
}

// ---------- reply keyboard ----------

export function KeyboardEditor({ value, onChange }: { value: Json | undefined; onChange: (v: Json) => void }) {
  const rows = rowsOf(value).map((r) => r.map((b) => (typeof b === "string" ? ({ text: b } as Btn) : b)));
  const set = (r: Btn[][]) =>
    onChange(r.filter((row) => row.length).map((row) => row.map((b) => (Object.keys(b).length === 1 ? String(b.text ?? "") : b))) as Json);
  return (
    <div className="kb">
      {rows.map((row, ri) => (
        <div key={ri} className="kb-row">
          {row.map((b, bi) => (
            <div key={bi} className="field-row">
              <input className="input" dir="auto" placeholder="متن" value={String(b.text ?? "")} onChange={(e) => set(rows.map((r, i) => (i === ri ? r.map((x, j) => (j === bi ? { ...x, text: e.target.value } : x)) : r)))} />
              <select
                className="input small"
                value={b.request_contact ? "contact" : b.request_location ? "location" : ""}
                onChange={(e) => {
                  const nb: Btn = { text: b.text ?? "" };
                  if (e.target.value === "contact") nb.request_contact = true;
                  if (e.target.value === "location") nb.request_location = true;
                  set(rows.map((r, i) => (i === ri ? r.map((x, j) => (j === bi ? nb : x)) : r)));
                }}
              >
                <option value="">متن عادی</option>
                <option value="contact">درخواست شماره</option>
                <option value="location">درخواست مکان</option>
              </select>
              <button type="button" className="btn-icon danger" onClick={() => set(rows.map((r, i) => (i === ri ? r.filter((_, j) => j !== bi) : r)))}>
                ×
              </button>
            </div>
          ))}
          <button type="button" className="btn-ghost small" onClick={() => set(rows.map((r, i) => (i === ri ? [...r, { text: "" }] : r)))}>
            + دکمه
          </button>
        </div>
      ))}
      <button type="button" className="btn-ghost small" onClick={() => set([...rows, [{ text: "" }]])}>
        + ردیف جدید
      </button>
    </div>
  );
}

// ---------- key / value ----------

export function VarsEditor({ value, onChange, vars, keyPlaceholder }: { value: Json | undefined; onChange: (v: Json) => void; vars?: VarGroup[]; keyPlaceholder?: string }) {
  const entries = Object.entries((value && typeof value === "object" && !Array.isArray(value) ? value : {}) as Record<string, Json>);
  const [rows, setRows] = useState<[string, string][]>(() => entries.map(([k, v]) => [k, typeof v === "string" ? v : JSON.stringify(v)]));
  const commit = (next: [string, string][]) => {
    setRows(next);
    const out: Record<string, Json> = {};
    for (const [k, v] of next) if (k.trim()) out[k.trim()] = v;
    onChange(out);
  };
  return (
    <div className="kv">
      {rows.map(([k, v], i) => (
        <div key={i} className="field-row">
          <input className="input mono key" dir="ltr" placeholder={keyPlaceholder ?? "نام"} value={k} onChange={(e) => commit(rows.map((r, j) => (j === i ? [e.target.value, r[1]] : r)))} />
          <TextField value={v} placeholder="مقدار" vars={vars} onChange={(nv) => commit(rows.map((r, j) => (j === i ? [r[0], nv] : r)))} />
          <button type="button" className="btn-icon danger" onClick={() => commit(rows.filter((_, j) => j !== i))}>
            ×
          </button>
        </div>
      ))}
      <button type="button" className="btn-ghost small" onClick={() => setRows([...rows, ["", ""]])}>
        + افزودن
      </button>
    </div>
  );
}

// ---------- json ----------

export function JsonEditor({ value, onChange, placeholder }: { value: Json | undefined; onChange: (v: Json | undefined) => void; placeholder?: string }) {
  const [text, setText] = useState(() => (value === undefined ? "" : JSON.stringify(value, null, 2)));
  const [err, setErr] = useState("");
  return (
    <div>
      <textarea
        className={`input mono${err ? " invalid" : ""}`}
        dir="ltr"
        rows={Math.min(12, Math.max(3, text.split("\n").length))}
        value={text}
        placeholder={placeholder}
        spellCheck={false}
        onChange={(e) => {
          setText(e.target.value);
          if (!e.target.value.trim()) {
            setErr("");
            onChange(undefined);
            return;
          }
          try {
            onChange(JSON.parse(e.target.value));
            setErr("");
          } catch (x) {
            setErr((x as Error).message);
          }
        }}
      />
      {err && <div className="field-err">JSON نامعتبر: {err}</div>}
    </div>
  );
}

// ---------- bot api method ----------

export function MethodPicker({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const { methods } = useContext(CatalogContext);
  const listId = useId();
  return (
    <>
      <input className="input mono" dir="ltr" list={listId} value={value} placeholder="sendMessage" onChange={(e) => onChange(e.target.value)} />
      <datalist id={listId}>
        {methods.map((m) => (
          <option key={m.name} value={m.name} />
        ))}
      </datalist>
    </>
  );
}

/** Converts form text to the JSON type a Bot API field expects. */
export function coerce(text: string, types: string[]): Json {
  const t = text.trim();
  if ((types.includes("Integer") || types.includes("Float")) && /^-?\d+(\.\d+)?$/.test(t)) return Number(t);
  if ((types.includes("Boolean") || types.includes("True")) && (t === "true" || t === "false")) return t === "true";
  return text;
}
