import { Handle, Position, type NodeProps } from "@xyflow/react";
import { memo, useContext } from "react";
import { CatalogContext } from "../context";
import { isTrigger, metaFor, outputsOf, summary, type FlowNode as FN } from "../convert";
import { categories } from "../types";

const outputLabels: Record<string, string> = {
  main: "",
  true: "بله",
  false: "خیر",
  default: "پیش‌فرض",
  item: "هر مورد",
  done: "پایان",
  error: "خطا",
};

function FlowNodeView({ id, data, selected }: NodeProps<FN>) {
  const { metas } = useContext(CatalogContext);
  const spec = data.spec;
  const meta = metaFor(metas, spec.type);
  const cat = categories[meta?.category ?? "other"] ?? categories.other;
  const trigger = isTrigger(spec.type);
  const outs = outputsOf(spec, meta);
  const allOuts = trigger ? outs : [...outs, "error"];
  const label = spec.type.startsWith("tg.") ? spec.type.slice(3) : meta?.label ?? spec.type;
  const sum = summary(spec);

  return (
    <div
      className={`fnode${selected ? " selected" : ""}${spec.disabled ? " disabled" : ""}${data.issue ? " issue-" + data.issue : ""}`}
      style={{ ["--cat" as string]: cat.color }}
      title={data.issueText}
      dir="rtl"
    >
      {!trigger && <Handle type="target" position={Position.Left} id="in" className="h-in" />}
      <div className="fnode-head">
        <span className="fnode-icon">{meta?.icon ?? "•"}</span>
        <span className="fnode-title">{spec.name || label}</span>
        {trigger && <span className="fnode-badge">شروع</span>}
      </div>
      <div className="fnode-body">
        {spec.name && <div className="fnode-type">{label}</div>}
        {sum && (
          <div className="fnode-sum" dir="auto">
            {sum}
          </div>
        )}
        <div className="fnode-id" dir="ltr">
          {id}
        </div>
      </div>
      <div className="fnode-outs">
        {allOuts.map((o) => (
          <div key={o} className={`fnode-out out-${o === "error" ? "error" : "x"}`}>
            <span dir="auto">{outputLabels[o] ?? o}</span>
            <Handle type="source" position={Position.Right} id={o} className={`h-out h-${o === "error" ? "error" : o}`} />
          </div>
        ))}
      </div>
    </div>
  );
}

export default memo(FlowNodeView);
