import { BaseEdge, EdgeLabelRenderer, getSmoothStepPath, useReactFlow, type EdgeProps } from "@xyflow/react";
import { X } from "lucide-react";
import { memo, useState } from "react";
import { BTN, outputLabel, type FlowNode } from "../convert";
import { outputLabels } from "./FlowNode";

function EdgeView({ id, source, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, sourceHandleId, selected, markerEnd }: EdgeProps) {
  const { deleteElements, getNode } = useReactFlow();
  const [hover, setHover] = useState(false);
  const [path, lx, ly] = getSmoothStepPath({ sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, borderRadius: 14, offset: 24 });
  const out = sourceHandleId ?? "main";
  // Button connections are labelled by the button chip on the node itself.
  const label = out.startsWith(BTN) ? "" : outputLabel((getNode(source) as FlowNode | undefined)?.data.spec, out, outputLabels);
  const kind = out === "error" ? "error" : out === "false" ? "false" : out === "true" ? "true" : out.startsWith(BTN) ? "btn" : "main";
  return (
    <>
      <g onMouseEnter={() => setHover(true)} onMouseLeave={() => setHover(false)}>
        <BaseEdge id={id} path={path} markerEnd={markerEnd} className={`edge edge-${kind}${selected ? " selected" : ""}`} interactionWidth={22} />
      </g>
      <EdgeLabelRenderer>
        <div
          className="edge-tools nodrag nopan"
          style={{ transform: `translate(-50%, -50%) translate(${lx}px, ${ly}px)` }}
          onMouseEnter={() => setHover(true)}
          onMouseLeave={() => setHover(false)}
        >
          {label && out !== "main" && kind !== "btn" && <span className={`edge-pill edge-pill-${kind}`}>{label}</span>}
          {(hover || selected) && (
            <button type="button" className="edge-del" title="حذف اتصال" onClick={() => deleteElements({ edges: [{ id }] })}>
              <X size={12} strokeWidth={2.5} />
            </button>
          )}
        </div>
      </EdgeLabelRenderer>
    </>
  );
}

export default memo(EdgeView);
