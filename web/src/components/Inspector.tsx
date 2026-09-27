import { useContext, useMemo, useState } from "react";
import { CatalogContext } from "../context";
import { isTrigger, metaFor, type FlowNode } from "../convert";
import { ButtonsEditor, JsonEditor, KeyboardEditor, MethodPicker, TagsInput, TextField, VarsEditor, baseVars, coerce, type VarGroup } from "../editors/fields";
import { categories, type BotField, type Issue, type Json, type Param, type WorkflowNode } from "../types";

interface Props {
  node: FlowNode;
  nodeIds: string[];
  variables: string[];
  issues: Issue[];
  onChange: (spec: WorkflowNode) => void;
  onRename: (from: string, to: string) => void;
  onDelete: () => void;
  onDuplicate: () => void;
}

const scalar = (types: string[]) => types.every((t) => ["Integer", "String", "Boolean", "Float", "True", "InputFile"].includes(t));

export default function Inspector({ node, nodeIds, variables, issues, onChange, onRename, onDelete, onDuplicate }: Props) {
  const { metas, methodMap } = useContext(CatalogContext);
  const spec = node.data.spec;
  const meta = metaFor(metas, spec.type);
  const params = spec.params ?? {};
  const cat = categories[meta?.category ?? "other"] ?? categories.other;
  const [idDraft, setIdDraft] = useState(node.id);
  const [idErr, setIdErr] = useState("");

  const vars: VarGroup[] = useMemo(
    () => [
      ...baseVars,
      { title: "متغیرها (vars)", items: [...variables.map((v) => ({ expr: `vars.${v}`, label: "متغیر workflow" })), { expr: "vars.item", label: "عضو فعلی حلقه" }, { expr: "vars.index", label: "شمارهٔ حلقه" }] },
      { title: "خروجی نودها", items: nodeIds.filter((i) => i !== node.id).map((i) => ({ expr: `nodes.${i}`, label: "نتیجهٔ نود" })) },
      { title: "وضعیت کاربر", items: [{ expr: "state.step", label: "مرحله (مثال)" }, { expr: "error.error", label: "متن خطا (در شاخهٔ خطا)" }] },
    ],
    [variables, nodeIds, node.id],
  );

  const setParam = (name: string, value: Json | undefined) => {
    const next = { ...params };
    if (value === undefined || value === "" || (Array.isArray(value) && value.length === 0)) delete next[name];
    else next[name] = value;
    onChange({ ...spec, params: next });
  };

  // tg.<method>: fields come straight from the Bot API spec.
  const tgMethod = spec.type.startsWith("tg.") ? methodMap[spec.type.slice(3)] : undefined;
  const passthrough = meta?.method ? methodMap[meta.method] : undefined;
  const known = new Set((meta?.params ?? []).map((p) => p.name));
  const extraKeys = passthrough ? Object.keys(params).filter((k) => !known.has(k)) : [];
  const addable = passthrough?.fields.filter((f) => !known.has(f.name) && !(f.name in params) && f.name !== "reply_markup") ?? [];
  const [adding, setAdding] = useState("");

  return (
    <div className="inspector">
      <div className="insp-head" style={{ ["--cat" as string]: cat.color }}>
        <span className="insp-icon">{meta?.icon}</span>
        <div>
          <div className="insp-title">{tgMethod ? tgMethod.name : meta?.label ?? spec.type}</div>
          <div className="insp-sub">
            {cat.title} · <code dir="ltr">{spec.type}</code>
          </div>
        </div>
      </div>

      {issues.length > 0 && (
        <div className="insp-issues">
          {issues.map((i, k) => (
            <div key={k} className={`issue issue-${i.level}`}>
              {i.level === "error" ? "⛔" : "⚠️"} {i.message}
            </div>
          ))}
        </div>
      )}

      <section className="insp-sec">
        <label className="field">
          <span className="field-label">شناسه</span>
          <input
            className={`input mono${idErr ? " invalid" : ""}`}
            dir="ltr"
            value={idDraft}
            onChange={(e) => {
              setIdDraft(e.target.value);
              setIdErr("");
            }}
            onBlur={() => {
              const v = idDraft.trim();
              if (v === node.id) return;
              if (!/^[a-zA-Z_][a-zA-Z0-9_]*$/.test(v)) setIdErr("فقط حروف انگلیسی، عدد و _");
              else if (nodeIds.includes(v)) setIdErr("تکراری است");
              else onRename(node.id, v);
            }}
          />
          {idErr && <span className="field-err">{idErr}</span>}
          <span className="field-help">در عبارت‌ها با nodes.{node.id} به خروجی این نود دسترسی دارید</span>
        </label>
        <label className="field">
          <span className="field-label">عنوان نمایشی</span>
          <input className="input" dir="auto" value={spec.name ?? ""} placeholder={meta?.label} onChange={(e) => onChange({ ...spec, name: e.target.value || undefined })} />
        </label>
      </section>

      <section className="insp-sec">
        {meta?.params.map((p) => (
          <ParamField key={`${node.id}:${p.name}`} param={p} value={params[p.name]} onChange={(v) => setParam(p.name, v)} vars={vars} />
        ))}
        {tgMethod && (
          <>
            {tgMethod.fields.length === 0 && <div className="muted">این متد پارامتری ندارد.</div>}
            {tgMethod.fields.map((f) => (
              <BotFieldEditor key={`${node.id}:${f.name}`} field={f} value={params[f.name]} onChange={(v) => setParam(f.name, v)} vars={vars} />
            ))}
          </>
        )}
        {spec.type.startsWith("tg.") && !tgMethod && <div className="field-err">متد {spec.type.slice(3)} در Bot API وجود ندارد</div>}
      </section>

      {passthrough && (
        <section className="insp-sec">
          <div className="sec-title">پارامترهای بیشتر تلگرام ({passthrough.name})</div>
          {extraKeys.map((k) => {
            const f = passthrough.fields.find((x) => x.name === k) ?? { name: k, types: ["String"], required: false };
            return (
              <div key={`${node.id}:x:${k}`} className="extra">
                <BotFieldEditor field={f} value={params[k]} onChange={(v) => setParam(k, v)} vars={vars} />
                <button type="button" className="btn-icon danger" title="حذف" onClick={() => setParam(k, undefined)}>
                  ×
                </button>
              </div>
            );
          })}
          <div className="field-row">
            <select className="input" value={adding} onChange={(e) => setAdding(e.target.value)}>
              <option value="">انتخاب پارامتر…</option>
              {addable.map((f) => (
                <option key={f.name} value={f.name}>
                  {f.name}
                </option>
              ))}
            </select>
            <button
              type="button"
              className="btn-ghost small"
              disabled={!adding}
              onClick={() => {
                const f = passthrough.fields.find((x) => x.name === adding)!;
                setParam(adding, f.types.includes("Boolean") ? true : scalar(f.types) ? "" : {});
                setAdding("");
              }}
            >
              افزودن
            </button>
          </div>
        </section>
      )}

      <section className="insp-sec">
        <div className="sec-title">گزینه‌ها</div>
        <label className="check">
          <input type="checkbox" checked={!!spec.disabled} onChange={(e) => onChange({ ...spec, disabled: e.target.checked || undefined })} />
          غیرفعال
        </label>
        {!isTrigger(spec.type) && (
          <label className="check">
            <input type="checkbox" checked={!!spec.continue_on_error} onChange={(e) => onChange({ ...spec, continue_on_error: e.target.checked || undefined })} />
            در صورت خطا ادامه بده (اگر خروجی «خطا» وصل نیست)
          </label>
        )}
      </section>

      <div className="insp-actions">
        <button type="button" className="btn-ghost" onClick={onDuplicate}>
          کپی نود
        </button>
        <button type="button" className="btn-danger" onClick={onDelete}>
          حذف نود
        </button>
      </div>
    </div>
  );
}

function Label({ p }: { p: { label: string; required?: boolean; help?: string } }) {
  return (
    <span className="field-label">
      {p.label}
      {p.required && <span className="req">*</span>}
    </span>
  );
}

export function ParamField({ param: p, value, onChange, vars }: { param: Param; value: Json | undefined; onChange: (v: Json | undefined) => void; vars: VarGroup[] }) {
  const str = value === undefined || value === null ? "" : typeof value === "string" ? value : JSON.stringify(value);
  let editor: React.ReactNode;
  switch (p.type) {
    case "textarea":
      editor = <TextField multiline value={str} placeholder={p.placeholder} onChange={onChange} vars={vars} />;
      break;
    case "expr":
      editor = <TextField multiline mono bare value={str} placeholder={p.placeholder} onChange={onChange} vars={vars} />;
      break;
    case "sql":
      editor = <TextField multiline mono value={str} placeholder={p.placeholder ?? "SELECT …"} onChange={onChange} />;
      break;
    case "number":
      editor = <input className="input" type="number" dir="ltr" value={str} placeholder={p.default !== undefined ? String(p.default) : ""} onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))} />;
      break;
    case "bool":
      return (
        <label className="check">
          <input type="checkbox" checked={value === true} onChange={(e) => onChange(e.target.checked || undefined)} />
          {p.label}
        </label>
      );
    case "select":
      editor = (
        <select className="input" value={str || (p.default as string) || ""} onChange={(e) => onChange(e.target.value || undefined)}>
          {p.options?.map((o) => (
            <option key={o} value={o}>
              {o || "پیش‌فرض"}
            </option>
          ))}
        </select>
      );
      break;
    case "tags":
      editor = (
        <TagsInput
          value={Array.isArray(value) ? value.map(String) : typeof value === "string" && value ? [value] : []}
          onChange={(v) => onChange(p.name === "commands" ? v.map((c) => c.replace(/^\//, "")) : v)}
          options={p.options}
          placeholder={p.placeholder}
        />
      );
      break;
    case "buttons":
      editor = <ButtonsEditor value={value} onChange={onChange} vars={vars} />;
      break;
    case "keyboard":
      editor = <KeyboardEditor value={value} onChange={onChange} />;
      break;
    case "json":
      editor = <JsonEditor value={value ?? p.default} onChange={onChange} placeholder={p.placeholder} />;
      break;
    case "vars":
      editor = <VarsEditor value={value} onChange={onChange} vars={vars} />;
      break;
    case "method":
      editor = <MethodPicker value={str} onChange={onChange} />;
      break;
    case "duration":
      editor = <TextField value={str} placeholder={p.placeholder ?? String(p.default ?? "")} onChange={onChange} list={["500ms", "1s", "5s", "10s", "30s", "1m", "5m", "1h"]} />;
      break;
    default:
      editor = <TextField value={str} placeholder={p.placeholder} onChange={onChange} vars={vars} />;
  }
  return (
    <div className="field">
      <Label p={p} />
      {editor}
      {p.help && <span className="field-help">{p.help}</span>}
    </div>
  );
}

function BotFieldEditor({ field: f, value, onChange, vars }: { field: BotField; value: Json | undefined; onChange: (v: Json | undefined) => void; vars: VarGroup[] }) {
  const label = { label: f.name, required: f.required };
  const typeHint = f.types.join(" | ");
  if (f.types.length === 1 && (f.types[0] === "Boolean" || f.types[0] === "True")) {
    return (
      <label className="check" title={typeHint}>
        <input type="checkbox" checked={value === true} onChange={(e) => onChange(e.target.checked || undefined)} />
        <code dir="ltr">{f.name}</code>
      </label>
    );
  }
  if (!scalar(f.types)) {
    return (
      <div className="field">
        <Label p={label} />
        <JsonEditor value={value} onChange={onChange} placeholder={typeHint} />
        <span className="field-help" dir="ltr">
          {typeHint}
        </span>
      </div>
    );
  }
  const str = value === undefined || value === null ? "" : typeof value === "string" ? value : JSON.stringify(value);
  return (
    <div className="field">
      <Label p={label} />
      <TextField
        value={str}
        placeholder={f.name === "chat_id" ? "{{ chat.id }}" : f.types.includes("InputFile") ? "file_id، لینک یا file:///مسیر" : typeHint}
        onChange={(v) => onChange(v === "" ? undefined : coerce(v, f.types))}
        vars={vars}
      />
      <span className="field-help" dir="ltr">
        {typeHint}
      </span>
    </div>
  );
}
