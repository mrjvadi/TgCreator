import { Handle, Position, type NodeProps } from "@xyflow/react";
import { CircleAlert, Plus, TriangleAlert, Zap } from "lucide-react";
import { memo, useContext } from "react";
import { CatalogContext, EditorContext } from "../context";
import { isTrigger, metaFor, outputsOf, summary, type FlowNode as FN } from "../convert";
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

function FlowNodeView({ id, data, selected }: NodeProps<FN>) {
  const { metas } = useContext(CatalogContext);
  const { addAfter } = useContext(EditorContext);
  const spec = data.spec;
  const meta = metaFor(metas, spec.type);
  const cat = categories[meta?.category ?? "other"] ?? categories.other;
  const trigger = isTrigger(spec.type);
  const outs = outputsOf(spec, meta);
  const connected = new Set(data.connected ?? []);
  const showError = !trigger && (selected || connected.has("error"));
  const title = spec.name || (spec.type.startsWith("tg.") ? spec.type.slice(3) : meta?.label ?? spec.type);
  const sub = summary(spec) || meta?.summary || "";
  const labeled = outs.length > 1 || outs[0] !== "main";

  return (
    <div
      className={`node${trigger ? " trigger" : ""}${selected ? " selected" : ""}${spec.disabled ? " disabled" : ""}${data.issue ? " has-" + data.issue : ""}`}
      style={{ ["--cat" as string]: cat.color, minHeight: Math.max(64, outs.length * 30 + 12) }}
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

      {outs.map((o, i) => {
        const top = `${((i + 1) / (outs.length + 1)) * 100}%`;
        return (
          <div key={o} className="node-out" style={{ top }}>
            <Handle type="source" position={Position.Right} id={o} className={`hd hd-out hd-${o}`} />
            <div className="out-ext">
              {labeled && <span className={`out-label out-${o}`}>{outputLabels[o] ?? o}</span>}
              {!connected.has(o) && (
                <>
                  <span className="out-stub" />
                  <button
                    type="button"
                    className="out-plus nodrag nopan"
                    title="افزودن نود بعدی"
                    onClick={(e) => {
                      e.stopPropagation();
                      addAfter(id, o);
                    }}
                  >
                    <Plus size={13} strokeWidth={2.5} />
                  </button>
                </>
              )}
            </div>
          </div>
        );
      })}

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
