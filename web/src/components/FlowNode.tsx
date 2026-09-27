import { Handle, Position, useUpdateNodeInternals, type NodeProps } from "@xyflow/react";
import { CircleAlert, Link2, Plus, TriangleAlert, Zap } from "lucide-react";
import { memo, useContext, useEffect } from "react";
import { CatalogContext, EditorContext } from "../context";
import { BTN, buttonsOf, isTrigger, metaFor, outputsOf, summary, type FlowNode as FN } from "../convert";
import { NodeIcon } from "../icons";
import { categories } from "../types";

export const outputLabels: Record<string, string> = {
  main: "",
  true: "بله",
  false: "خیر",
  default: "بقیه",
  item: "هر مورد",
  done: "پایان",
  error: "خطا",
};

function OutHandle({ nodeId, id, connected, label, labelClass }: { nodeId: string; id: string; connected: boolean; label?: string; labelClass?: string }) {
  const { addAfter } = useContext(EditorContext);
  return (
    <>
      <Handle type="source" position={Position.Right} id={id} className={`hd hd-out hd-${id.startsWith(BTN) ? "btn" : id}`} />
      <div className="out-ext">
        {label && <span className={`out-label ${labelClass ?? ""}`}>{label}</span>}
        {!connected && (
          <>
            <span className="out-stub" />
            <button
              type="button"
              className="out-plus nodrag nopan"
              title="افزودن نود بعدی"
              onClick={(e) => {
                e.stopPropagation();
                addAfter(nodeId, id);
              }}
            >
              <Plus size={13} strokeWidth={2.5} />
            </button>
          </>
        )}
      </div>
    </>
  );
}

function FlowNodeView({ id, data, selected }: NodeProps<FN>) {
  const { metas } = useContext(CatalogContext);
  const spec = data.spec;
  const meta = metaFor(metas, spec.type);
  const cat = categories[meta?.category ?? "other"] ?? categories.other;
  const trigger = isTrigger(spec.type);
  const buttons = buttonsOf(spec, meta);
  const outs = outputsOf(spec, meta).filter((o) => !o.startsWith(BTN));
  const connected = new Set(data.connected ?? []);
  const showError = !trigger && (selected || connected.has("error"));
  const title = spec.name || (spec.type.startsWith("tg.") ? spec.type.slice(3) : meta?.label ?? spec.type);
  const sub = summary(spec) || meta?.summary || "";
  const isMsg = buttons.length > 0;
  const labeled = isMsg || outs.length > 1 || outs[0] !== "main";

  // Handles come and go with buttons, switch cases and the error output;
  // React Flow must re-measure them or edges to new handles are not drawn.
  const updateInternals = useUpdateNodeInternals();
  const handleKey = [...outs, ...buttons.map((b) => b.data ?? ""), showError ? "error" : ""].join("|");
  useEffect(() => updateInternals(id), [handleKey, id, updateInternals]);

  return (
    <div
      className={`node${trigger ? " trigger" : ""}${isMsg ? " msg" : ""}${selected ? " selected" : ""}${spec.disabled ? " disabled" : ""}${data.issue ? " has-" + data.issue : ""}`}
      style={{ ["--cat" as string]: cat.color }}
    >
      {trigger && (
        <span className="node-trigger-badge" title="شروع‌کننده">
          <Zap size={11} fill="currentColor" />
        </span>
      )}
      {data.issue && (
        <span className={`node-issue ${data.issue}`} title={data.issueText}>
          {data.issue === "error" ? <CircleAlert size={14} /> : <TriangleAlert size={14} />}
        </span>
      )}

      <div className="node-main" style={{ minHeight: Math.max(42, outs.length * 30 - 18) }}>
        {!trigger && <Handle type="target" position={Position.Left} id="in" className="hd hd-in" />}
        <div className="node-icon">
          <NodeIcon name={meta?.icon} size={19} />
        </div>
        <div className="node-text">
          <div className="node-title" dir="auto">
            {title}
          </div>
          {sub && (
            <div className="node-sub" dir="auto">
              {sub}
            </div>
          )}
        </div>
        {outs.map((o, i) => (
          <div key={o} className="node-out" style={{ top: `${((i + 1) / (outs.length + 1)) * 100}%` }}>
            <OutHandle
              nodeId={id}
              id={o}
              connected={connected.has(o)}
              label={labeled ? (isMsg && o === "main" ? "بعد از ارسال" : outputLabels[o] ?? o) : undefined}
              labelClass={`out-${o}`}
            />
          </div>
        ))}
      </div>

      {isMsg && (
        <div className="btn-outs">
          {buttons.map((b, i) => (
            <div key={i} className={`btn-out${b.url ? " url" : ""}${b.data && connected.has(BTN + b.data) ? " linked" : ""}`}>
              <span className="btn-chip" dir="auto" title={b.data ? `callback_data: ${b.data}` : "دکمهٔ لینک"}>
                {b.url && <Link2 size={11} />}
                {b.text || "بدون متن"}
              </span>
              {b.data && (
                <div className="node-out" style={{ top: "50%" }}>
                  <OutHandle nodeId={id} id={BTN + b.data} connected={connected.has(BTN + b.data)} />
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {showError && (
        <div className="node-err">
          <Handle type="source" position={Position.Bottom} id="error" className="hd hd-error" />
          <span className="out-label out-error err-label">خطا</span>
        </div>
      )}
    </div>
  );
}

export default memo(FlowNodeView);
