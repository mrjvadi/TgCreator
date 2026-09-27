import { CircleAlert, Copy, Plus, Trash2, TriangleAlert, X } from "lucide-react";
import { useContext, useMemo, useState } from "react";
import { CatalogContext } from "../context";
import { isTrigger, metaFor, outputsOf, type FlowNode } from "../convert";
import { ButtonsEditor, JsonEditor, KeyboardEditor, MethodPicker, TagsInput, TextField, VarsEditor, baseVars, coerce, type VarGroup } from "../editors/fields";
import { NodeIcon } from "../icons";
import { categories, type BotField, type Issue, type Json, type Param, type WorkflowNode } from "../types";
import { outputLabels } from "./FlowNode";

interface Props {
  node: FlowNode;
  nodeIds: string[];
  variables: string[];
  issues: Issue[];
  description: string;
  onChange: (spec: WorkflowNode) => void;
  onRename: (from: string, to: string) => void;
  onDelete: () => void;
  onDuplicate: () => void;
  onClose: () => void;
}

type Tab = "params" | "settings" | "docs";
const scalar = (types: string[]) => types.every((t) => ["Integer", "String", "Boolean", "Float", "True", "InputFile"].includes(t));

const outputHelp: Record<string, string> = {
  main: "بعد از اجرای موفق",
  true: "وقتی شرط برقرار است",
  false: "وقتی شرط برقرار نیست",
  default: "وقتی هیچ حالتی نخورد",
  item: "یک بار برای هر عضو لیست (vars.item)",
  done: "بعد از تمام شدن حلقه",
  error: "اگر این نود خطا بدهد (error.error)",
};

export function Switch({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: React.ReactNode }) {
  return (
    <label className="switch-row">
      <span>{label}</span>
      <button type="button" role="switch" aria-checked={checked} className={`switch${checked ? " on" : ""}`} onClick={() => onChange(!checked)}>
        <span />
      </button>
    </label>
  );
}

export default function Inspector({ node, nodeIds, variables, issues, description, onChange, onRename, onDelete, onDuplicate, onClose }: Props) {
  const { metas, methodMap } = useContext(CatalogContext);
  const spec = node.data.spec;
  const meta = metaFor(metas, spec.type);
  const params = spec.params ?? {};
  const cat = categories[meta?.category ?? "other"] ?? categories.other;
  const [tab, setTab] = useState<Tab>("params");
  const [idDraft, setIdDraft] = useState(node.id);
  const [idErr, setIdErr] = useState("");
  const [adding, setAdding] = useState("");

  const vars: VarGroup[] = useMemo(
    () => [
      ...baseVars,
      {
        title: "متغیرها (vars)",
        items: [...variables.map((v) => ({ expr: `vars.${v}`, label: "متغیر workflow" })), { expr: "vars.item", label: "عضو فعلی حلقه" }, { expr: "vars.index", label: "شمارهٔ حلقه" }],
      },
      { title: "خروجی نودهای دیگر", items: nodeIds.filter((i) => i !== node.id).map((i) => ({ expr: `nodes.${i}`, label: "نتیجه" })) },
      { title: "وضعیت و خطا", items: [{ expr: "state.step", label: "وضعیت کاربر (مثال)" }, { expr: "error.error", label: "متن خطا (در مسیر خطا)" }] },
    ],
    [variables, nodeIds, node.id],
  );

  const setParam = (name: string, value: Json | undefined) => {
    const next = { ...params };
    if (value === undefined || value === "" || (Array.isArray(value) && value.length === 0)) delete next[name];
    else next[name] = value;
    onChange({ ...spec, params: next });
  };

  const tgMethod = spec.type.startsWith("tg.") ? methodMap[spec.type.slice(3)] : undefined;
  const passthrough = meta?.method ? methodMap[meta.method] : undefined;
  const known = new Set((meta?.params ?? []).map((p) => p.name));
  const extraKeys = passthrough ? Object.keys(params).filter((k) => !known.has(k)) : [];
  const addable = passthrough?.fields.filter((f) => !known.has(f.name) && !(f.name in params) && f.name !== "reply_markup") ?? [];
  const title = tgMethod ? tgMethod.name : meta?.label ?? spec.type;
  const outs = [...outputsOf(spec, meta), ...(isTrigger(spec.type) ? [] : ["error"])];

  return (
    <div className="insp" style={{ ["--cat" as string]: cat.color }}>
      <div className="insp-head">
        <div className="insp-icon">
          <NodeIcon name={meta?.icon} size={20} />
        </div>
        <div className="insp-titles">
          <input
            className="insp-name"
            dir="auto"
            value={spec.name ?? ""}
            placeholder={title}
            onChange={(e) => onChange({ ...spec, name: e.target.value || undefined })}
            aria-label="عنوان نود"
          />
          <div className="insp-type">
            <span className="chip" style={{ color: cat.color }}>
              {cat.title}
            </span>
            <code dir="ltr">{node.id}</code>
          </div>
        </div>
        <button type="button" className="icon-btn" onClick={onClose} aria-label="بستن">
          <X size={16} />
        </button>
      </div>

      <div className="tabs-line">
        {(
          [
            ["params", "پارامترها"],
            ["settings", "تنظیمات"],
            ["docs", "راهنما"],
          ] as [Tab, string][]
        ).map(([k, l]) => (
          <button type="button" key={k} className={tab === k ? "on" : ""} onClick={() => setTab(k)}>
            {l}
          </button>
        ))}
      </div>

      <div className="insp-body">
        {issues.length > 0 && (
          <div className="insp-issues">
            {issues.map((i, k) => (
              <div key={k} className={`issue-line ${i.level}`}>
                {i.level === "error" ? <CircleAlert size={14} /> : <TriangleAlert size={14} />}
                <span>{i.message}</span>
              </div>
            ))}
          </div>
        )}

        {tab === "params" && (
          <>
            {meta?.params.map((p) => (
              <ParamField key={`${node.id}:${p.name}`} param={p} value={params[p.name]} onChange={(v) => setParam(p.name, v)} vars={vars} />
            ))}
            {tgMethod?.fields.map((f) => (
              <BotFieldEditor key={`${node.id}:${f.name}`} field={f} value={params[f.name]} onChange={(v) => setParam(f.name, v)} vars={vars} />
            ))}
            {tgMethod && tgMethod.fields.length === 0 && <div className="empty-note">این متد پارامتری ندارد.</div>}
            {spec.type.startsWith("tg.") && !tgMethod && <div className="field-err">متد {spec.type.slice(3)} در Bot API وجود ندارد</div>}
            {!meta?.params.length && !tgMethod && <div className="empty-note">این نود تنظیمی ندارد.</div>}

            {passthrough && (
              <div className="insp-section">
                <div className="section-title">
                  پارامترهای بیشتر تلگرام <code dir="ltr">{passthrough.name}</code>
                </div>
                {extraKeys.map((k) => {
                  const f = passthrough.fields.find((x) => x.name === k) ?? { name: k, types: ["String"], required: false };
                  return (
                    <div key={`${node.id}:x:${k}`} className="extra">
                      <BotFieldEditor field={f} value={params[k]} onChange={(v) => setParam(k, v)} vars={vars} />
                      <button type="button" className="icon-btn danger" title="حذف" onClick={() => setParam(k, undefined)}>
                        <Trash2 size={14} />
                      </button>
                    </div>
                  );
                })}
                <div className="add-param">
                  <select className="input" value={adding} onChange={(e) => setAdding(e.target.value)}>
                    <option value="">انتخاب پارامتر ({addable.length})…</option>
                    {addable.map((f) => (
                      <option key={f.name} value={f.name}>
                        {f.name}
                      </option>
                    ))}
                  </select>
                  <button
                    type="button"
                    className="btn btn-soft"
                    disabled={!adding}
                    onClick={() => {
                      const f = passthrough.fields.find((x) => x.name === adding)!;
                      setParam(adding, f.types.includes("Boolean") ? true : scalar(f.types) ? "" : {});
                      setAdding("");
                    }}
                  >
                    <Plus size={14} /> افزودن
                  </button>
                </div>
              </div>
            )}
          </>
        )}

        {tab === "settings" && (
          <>
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
                  else if (nodeIds.includes(v)) setIdErr("این شناسه تکراری است");
                  else onRename(node.id, v);
                }}
              />
              {idErr ? <span className="field-err">{idErr}</span> : <span className="field-help">خروجی این نود در بقیهٔ نودها: nodes.{node.id}</span>}
            </label>
            <Switch checked={!!spec.disabled} onChange={(v) => onChange({ ...spec, disabled: v || undefined })} label="غیرفعال" />
            {!isTrigger(spec.type) && (
              <Switch
                checked={!!spec.continue_on_error}
                onChange={(v) => onChange({ ...spec, continue_on_error: v || undefined })}
                label={
                  <>
                    ادامه با وجود خطا
                    <small>اگر خروجی «خطا» وصل نباشد، مسیر اصلی ادامه پیدا می‌کند</small>
                  </>
                }
              />
            )}
            <div className="insp-actions">
              <button type="button" className="btn btn-soft" onClick={onDuplicate}>
                <Copy size={14} /> کپی
              </button>
              <button type="button" className="btn btn-danger-soft" onClick={onDelete}>
                <Trash2 size={14} /> حذف نود
              </button>
            </div>
          </>
        )}

        {tab === "docs" && (
          <div className="docs">
            <p>{meta?.summary}</p>
            {description && (
              <p className="muted" dir="ltr">
                {description}
              </p>
            )}
            <div className="section-title">خروجی‌ها</div>
            <ul className="out-list">
              {outs.map((o) => (
                <li key={o}>
                  <span className={`out-label out-${o}`}>{outputLabels[o] || "اصلی"}</span>
                  {outputHelp[o] ?? `وقتی مقدار برابر «${o}» باشد`}
                </li>
              ))}
            </ul>
            <div className="section-title">استفاده از نتیجه</div>
            <p>
              در نودهای بعدی با <code dir="ltr">{`{{ nodes.${node.id} }}`}</code> به خروجی این نود دسترسی دارید؛ مثلاً{" "}
              <code dir="ltr">{`{{ nodes.${node.id}.message_id }}`}</code>.
            </p>
          </div>
        )}
      </div>
    </div>
  );
}

function FieldLabel({ label, required, hint }: { label: React.ReactNode; required?: boolean; hint?: string }) {
  return (
    <span className="field-label">
      {label}
      {required && <span className="req">*</span>}
      {hint && <span className="field-hint">{hint}</span>}
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
      editor = <TextField multiline mono bare rows={2} value={str} placeholder={p.placeholder} onChange={onChange} vars={vars} />;
      break;
    case "sql":
      editor = <TextField multiline mono rows={4} value={str} placeholder={p.placeholder ?? "SELECT …"} onChange={onChange} />;
      break;
    case "number":
      editor = <input className="input" type="number" dir="ltr" value={str} placeholder={p.default !== undefined ? String(p.default) : ""} onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))} />;
      break;
    case "bool":
      return <Switch checked={value === true} onChange={(v) => onChange(v || undefined)} label={p.label} />;
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
      <FieldLabel label={p.label} required={p.required} />
      {editor}
      {p.help && <span className="field-help">{p.help}</span>}
    </div>
  );
}

function BotFieldEditor({ field: f, value, onChange, vars }: { field: BotField; value: Json | undefined; onChange: (v: Json | undefined) => void; vars: VarGroup[] }) {
  const typeHint = f.types.join(" | ");
  const label = <code dir="ltr">{f.name}</code>;
  if (f.types.length === 1 && (f.types[0] === "Boolean" || f.types[0] === "True")) {
    return <Switch checked={value === true} onChange={(v) => onChange(v || undefined)} label={label} />;
  }
  if (!scalar(f.types)) {
    return (
      <div className="field">
        <FieldLabel label={label} required={f.required} hint={typeHint} />
        <JsonEditor value={value} onChange={onChange} placeholder={typeHint} />
      </div>
    );
  }
  const str = value === undefined || value === null ? "" : typeof value === "string" ? value : JSON.stringify(value);
  return (
    <div className="field">
      <FieldLabel label={label} required={f.required} hint={typeHint} />
      <TextField
        value={str}
        placeholder={f.name === "chat_id" ? "{{ chat.id }}" : f.types.includes("InputFile") ? "file_id، لینک یا file:///مسیر" : ""}
        onChange={(v) => onChange(v === "" ? undefined : coerce(v, f.types))}
        vars={vars}
      />
    </div>
  );
}
