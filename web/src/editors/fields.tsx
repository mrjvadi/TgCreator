import { Braces, Link2, MousePointerClick, Plus, Search, Trash2, X } from "lucide-react";
import { useContext, useEffect, useId, useRef, useState } from "react";
import { CatalogContext } from "../context";
import { autoCallbackData } from "../convert";
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
      { expr: "args[0]", label: "آرگومان اول دستور" },
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
      { expr: "len(text)", label: "طول متن" },
      { expr: "now().Unix()", label: "زمان فعلی (ثانیه)" },
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
      <button type="button" className={`var-btn${open ? " on" : ""}`} title="درج متغیر" onClick={() => setOpen(!open)}>
        <Braces size={13} />
      </button>
      {open && (
        <div className="pop varmenu-pop">
          <div className="pop-search">
            <Search size={14} />
            <input autoFocus placeholder="جست‌وجوی متغیر…" value={q} onChange={(e) => setQ(e.target.value)} />
          </div>
          <div className="varmenu-list">
            {groups.map((g) => {
              const items = g.items.filter((i) => match(i.expr) || match(i.label));
              if (!items.length) return null;
              return (
                <div key={g.title}>
                  <div className="pop-title">{g.title}</div>
                  {items.map((i) => (
                    <button
                      type="button"
                      key={i.expr}
                      className="pop-item"
                      onClick={() => {
                        onPick(i.expr);
                        setOpen(false);
                      }}
                    >
                      <span>{i.label}</span>
                      <code dir="ltr">{i.expr}</code>
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
  rows?: number;
}) {
  const ref = useRef<HTMLInputElement & HTMLTextAreaElement>(null);
  const listId = useId();
  const pick = (expr: string) => {
    props.onChange(insertAt(ref.current, props.value, props.bare ? expr : `{{ ${expr} }}`));
    requestAnimationFrame(() => ref.current?.focus());
  };
  const common = {
    ref,
    className: `input${props.mono ? " mono" : ""}${props.vars ? " with-var" : ""}`,
    value: props.value,
    placeholder: props.placeholder,
    dir: props.mono ? "ltr" : "auto",
    onChange: (e: React.ChangeEvent<HTMLInputElement & HTMLTextAreaElement>) => props.onChange(e.target.value),
  };
  return (
    <div className="tf">
      {props.multiline ? (
        <textarea {...common} rows={props.rows ?? (props.mono ? 3 : 4)} spellCheck={!props.mono} />
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
            <X size={11} strokeWidth={2.5} />
          </button>
        </span>
      ))}
      <input
        className="tags-input"
        value={draft}
        list={options ? listId : undefined}
        placeholder={value.length ? "" : placeholder ?? "بنویسید و Enter بزنید"}
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

// ---------- inline keyboard (Telegram-like preview) ----------

type Btn = Record<string, Json>;
const btnKinds: [string, string][] = [
  ["callback_data", "عملیات"],
  ["url", "لینک"],
  ["web_app", "مینی‌اپ"],
  ["switch_inline_query_current_chat", "inline"],
];

function rowsOf(v: Json | undefined): Btn[][] {
  if (!Array.isArray(v)) return [];
  return v.map((r) => (Array.isArray(r) ? (r as Btn[]) : [r as Btn]));
}

function kindOf(b: Btn): string {
  return btnKinds.find(([k]) => k in b)?.[0] ?? "callback_data";
}

export function ButtonsEditor({ value, onChange, vars, nodeId }: { value: Json | undefined; onChange: (v: Json | undefined) => void; vars: VarGroup[]; nodeId?: string }) {
  const rows = rowsOf(value);
  const used = new Set(rows.flat().map((b) => String(b.callback_data ?? "")));
  const newData = () => autoCallbackData(nodeId ?? "btn", used);
  const [sel, setSel] = useState<[number, number] | null>(null);
  const set = (r: Btn[][]) => {
    const clean = r.filter((row) => row.length);
    onChange(clean.length ? (clean as Json) : undefined);
  };
  const update = (ri: number, bi: number, b: Btn) => set(rows.map((row, i) => (i === ri ? row.map((x, j) => (j === bi ? b : x)) : row)));
  const add = (ri: number) => {
    const b = { text: "دکمه", callback_data: newData() };
    const next = ri === rows.length ? [...rows, [b]] : rows.map((r, i) => (i === ri ? [...r, b] : r));
    set(next);
    setSel([ri, ri === rows.length ? 0 : rows[ri].length]);
  };
  const cur = sel && rows[sel[0]]?.[sel[1]];
  const kind = cur ? kindOf(cur) : "callback_data";
  const raw = cur?.[kind];
  const val = kind === "web_app" ? String((raw as { url?: string })?.url ?? "") : String(raw ?? "");

  return (
    <div className="kbd">
      <div className="kbd-preview">
        {rows.map((row, ri) => (
          <div key={ri} className="kbd-row">
            {row.map((b, bi) => (
              <button type="button" key={bi} className={`kbd-btn${sel?.[0] === ri && sel?.[1] === bi ? " sel" : ""}`} onClick={() => setSel([ri, bi])}>
                {kindOf(b) === "url" && <Link2 size={11} />}
                <span dir="auto">{String(b.text ?? "") || "بدون متن"}</span>
              </button>
            ))}
            <button type="button" className="kbd-add" title="دکمه در همین ردیف" onClick={() => add(ri)}>
              <Plus size={13} />
            </button>
          </div>
        ))}
        <button type="button" className="kbd-add-row" onClick={() => add(rows.length)}>
          <Plus size={13} /> ردیف جدید
        </button>
      </div>
      {cur && sel && (
        <div className="kbd-edit">
          <div className="kbd-edit-head">
            <span>ویرایش دکمه</span>
            <button
              type="button"
              className="icon-btn danger"
              title="حذف دکمه"
              onClick={() => {
                set(rows.map((r, i) => (i === sel[0] ? r.filter((_, j) => j !== sel[1]) : r)));
                setSel(null);
              }}
            >
              <Trash2 size={14} />
            </button>
          </div>
          <TextField value={String(cur.text ?? "")} placeholder="متن دکمه" onChange={(v) => update(sel[0], sel[1], { ...cur, text: v })} vars={vars} />
          <div className="seg">
            {btnKinds.map(([k, l]) => (
              <button
                type="button"
                key={k}
                className={kind === k ? "on" : ""}
                onClick={() => {
                  const nb: Btn = { text: cur.text ?? "" };
                  const v = k === "callback_data" && !val ? newData() : val;
                  nb[k] = k === "web_app" ? { url: v } : v;
                  update(sel[0], sel[1], nb);
                }}
              >
                {l}
              </button>
            ))}
          </div>
          <TextField
            value={val}
            mono={kind !== "switch_inline_query_current_chat"}
            placeholder={kind === "callback_data" ? "مثلاً buy:{{ from.id }}" : kind === "switch_inline_query_current_chat" ? "متن پیش‌فرض" : "https://…"}
            onChange={(v) => update(sel[0], sel[1], { ...cur, [kind]: kind === "web_app" ? { url: v } : v })}
            vars={vars}
          />
          {kind === "callback_data" && (
            <div className="hint">
              <MousePointerClick size={12} /> روی بوم، از کنار همین دکمه به نود بعدی وصل کنید
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ---------- reply keyboard ----------

export function KeyboardEditor({ value, onChange }: { value: Json | undefined; onChange: (v: Json | undefined) => void }) {
  const rows = rowsOf(value).map((r) => r.map((b) => (typeof b === "string" ? ({ text: b } as Btn) : b)));
  const [sel, setSel] = useState<[number, number] | null>(null);
  const set = (r: Btn[][]) => {
    const clean = r.filter((row) => row.length).map((row) => row.map((b) => (Object.keys(b).length === 1 ? String(b.text ?? "") : b)));
    onChange(clean.length ? (clean as Json) : undefined);
  };
  const add = (ri: number) => {
    set(ri === rows.length ? [...rows, [{ text: "گزینه" }]] : rows.map((r, i) => (i === ri ? [...r, { text: "گزینه" }] : r)));
    setSel([ri, ri === rows.length ? 0 : rows[ri].length]);
  };
  const cur = sel && rows[sel[0]]?.[sel[1]];
  const kind = cur?.request_contact ? "contact" : cur?.request_location ? "location" : "";
  const update = (b: Btn) => sel && set(rows.map((r, i) => (i === sel[0] ? r.map((x, j) => (j === sel[1] ? b : x)) : r)));
  return (
    <div className="kbd reply">
      <div className="kbd-preview">
        {rows.map((row, ri) => (
          <div key={ri} className="kbd-row">
            {row.map((b, bi) => (
              <button type="button" key={bi} className={`kbd-btn${sel?.[0] === ri && sel?.[1] === bi ? " sel" : ""}`} onClick={() => setSel([ri, bi])}>
                <span dir="auto">{String(b.text ?? "") || "بدون متن"}</span>
              </button>
            ))}
            <button type="button" className="kbd-add" onClick={() => add(ri)}>
              <Plus size={13} />
            </button>
          </div>
        ))}
        <button type="button" className="kbd-add-row" onClick={() => add(rows.length)}>
          <Plus size={13} /> ردیف جدید
        </button>
      </div>
      {cur && sel && (
        <div className="kbd-edit">
          <div className="kbd-edit-head">
            <span>ویرایش دکمه</span>
            <button
              type="button"
              className="icon-btn danger"
              onClick={() => {
                set(rows.map((r, i) => (i === sel[0] ? r.filter((_, j) => j !== sel[1]) : r)));
                setSel(null);
              }}
            >
              <Trash2 size={14} />
            </button>
          </div>
          <input className="input" dir="auto" value={String(cur.text ?? "")} onChange={(e) => update({ ...cur, text: e.target.value })} />
          <div className="seg">
            {[
              ["", "متن عادی"],
              ["contact", "درخواست شماره"],
              ["location", "درخواست مکان"],
            ].map(([k, l]) => (
              <button
                type="button"
                key={k}
                className={kind === k ? "on" : ""}
                onClick={() => {
                  const nb: Btn = { text: cur.text ?? "" };
                  if (k === "contact") nb.request_contact = true;
                  if (k === "location") nb.request_location = true;
                  update(nb);
                }}
              >
                {l}
              </button>
            ))}
          </div>
        </div>
      )}
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
        <div key={i} className="kv-row">
          <input className="input mono kv-key" dir="ltr" placeholder={keyPlaceholder ?? "نام"} value={k} onChange={(e) => commit(rows.map((r, j) => (j === i ? [e.target.value, r[1]] : r)))} />
          <TextField value={v} placeholder="مقدار" vars={vars} onChange={(nv) => commit(rows.map((r, j) => (j === i ? [r[0], nv] : r)))} />
          <button type="button" className="icon-btn danger" onClick={() => commit(rows.filter((_, j) => j !== i))} aria-label="حذف">
            <Trash2 size={14} />
          </button>
        </div>
      ))}
      <button type="button" className="add-line" onClick={() => setRows([...rows, ["", ""]])}>
        <Plus size={13} /> افزودن
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
        className={`input mono code${err ? " invalid" : ""}`}
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
