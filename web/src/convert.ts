import dagre from "@dagrejs/dagre";
import type { Edge, Node } from "@xyflow/react";
import type { NodeMeta, NodeType, Workflow, WorkflowNode } from "./types";

export interface FlowData extends Record<string, unknown> {
  spec: WorkflowNode;
  issue?: "error" | "warning";
  issueText?: string;
  running?: boolean;
}
export type FlowNode = Node<FlowData, "tg">;
export type MetaMap = Record<string, NodeType>;

export const isTrigger = (type: string) => type.startsWith("trigger.");

export function metaFor(metas: MetaMap, type: string): NodeMeta | undefined {
  return metas[type]?.meta ?? (type.startsWith("tg.") ? metas["tg."]?.meta : undefined);
}

/** Outputs of a node, including switch cases. "error" is added by the UI. */
export function outputsOf(spec: WorkflowNode, meta?: NodeMeta): string[] {
  if (!meta) return ["main"];
  const outs = [...meta.outputs];
  if (meta.case_outputs) {
    const cases = spec.params?.[meta.case_outputs];
    if (Array.isArray(cases)) outs.unshift(...cases.map(String).filter(Boolean));
  }
  return outs;
}

export function edgeFor(source: string, output: string, target: string): Edge {
  return {
    id: `${source}|${output}|${target}`,
    source,
    sourceHandle: output,
    target,
    targetHandle: "in",
    label: output === "main" ? undefined : output,
    className: `edge-${output === "error" ? "error" : output === "false" ? "false" : output === "true" ? "true" : "main"}`,
  };
}

export type WorkflowRest = Omit<Workflow, "nodes" | "connections">;

export function fromWorkflow(wf: Workflow): { rest: WorkflowRest; nodes: FlowNode[]; edges: Edge[]; needsLayout: boolean } {
  const { nodes: specs = [], connections = {}, ...rest } = wf;
  let needsLayout = false;
  const nodes: FlowNode[] = specs.map((n) => {
    const { position, ...spec } = n;
    if (!position) needsLayout = true;
    return { id: n.id, type: "tg", position: position ? { x: position[0], y: position[1] } : { x: 0, y: 0 }, data: { spec } };
  });
  const ids = new Set(nodes.map((n) => n.id));
  const edges: Edge[] = [];
  for (const [from, outs] of Object.entries(connections)) {
    for (const [out, targets] of Object.entries(outs)) {
      for (const to of targets) if (ids.has(from) && ids.has(to)) edges.push(edgeFor(from, out, to));
    }
  }
  return { rest: { ...rest, name: rest.name ?? "ربات جدید", bot: rest.bot ?? { token: "${BOT_TOKEN}" } }, nodes, edges, needsLayout };
}

export function toWorkflow(rest: WorkflowRest, nodes: FlowNode[], edges: Edge[]): Workflow {
  const connections: Workflow["connections"] = {};
  const sorted = [...edges].sort((a, b) => a.id.localeCompare(b.id));
  for (const e of sorted) {
    const out = e.sourceHandle ?? "main";
    (connections[e.source] ??= {})[out] ??= [];
    const list = connections[e.source][out];
    if (!list.includes(e.target)) list.push(e.target);
  }
  return {
    ...rest,
    version: rest.version ?? 1,
    nodes: nodes.map((n) => {
      const spec: WorkflowNode = { ...n.data.spec, id: n.id, position: [Math.round(n.position.x), Math.round(n.position.y)] };
      if (spec.params && Object.keys(spec.params).length === 0) delete spec.params;
      return spec;
    }),
    connections,
  };
}

/** Left-to-right layered layout (used for workflows without positions). */
export function layout(nodes: FlowNode[], edges: Edge[]): FlowNode[] {
  const g = new dagre.graphlib.Graph();
  g.setGraph({ rankdir: "LR", nodesep: 28, ranksep: 90, marginx: 20, marginy: 20 });
  g.setDefaultEdgeLabel(() => ({}));
  for (const n of nodes) g.setNode(n.id, { width: n.measured?.width ?? 230, height: n.measured?.height ?? 90 });
  for (const e of edges) g.setEdge(e.source, e.target);
  dagre.layout(g);
  return nodes.map((n) => {
    const p = g.node(n.id);
    const w = n.measured?.width ?? 230;
    const h = n.measured?.height ?? 90;
    return { ...n, position: { x: p.x - w / 2, y: p.y - h / 2 } };
  });
}

export function uniqueId(base: string, taken: Set<string>): string {
  const clean = base.replace(/^(trigger|telegram|logic|state|redis|db|http|tg)\./, "").replace(/[^a-zA-Z0-9_]/g, "_") || "node";
  for (let i = 1; ; i++) {
    const id = `${clean}_${i}`;
    if (!taken.has(id)) return id;
  }
}

/** Short human summary shown on the node card. */
export function summary(spec: WorkflowNode): string {
  const p = spec.params ?? {};
  const pick = (...keys: string[]) => {
    for (const k of keys) {
      const v = p[k];
      if (v === undefined || v === null || v === "") continue;
      if (Array.isArray(v)) return v.map((x) => (typeof x === "string" ? x : JSON.stringify(x))).join("، ");
      if (typeof v === "object") return JSON.stringify(v);
      return String(v);
    }
    return "";
  };
  let s = "";
  switch (spec.type) {
    case "trigger.command":
      s = pick("commands") ? "/" + pick("commands").split("، ").join(" /") : "";
      break;
    case "trigger.callback":
      s = pick("data", "prefix", "regex");
      break;
    case "trigger.update":
      s = pick("on");
      break;
    case "trigger.message":
      s = pick("condition", "has", "text", "contains", "regex", "updates");
      break;
    case "logic.if":
      s = pick("condition");
      break;
    case "logic.switch":
      s = pick("value");
      break;
    case "logic.set":
    case "state.set":
      s = Object.keys((p.vars ?? p.values ?? {}) as object).join("، ");
      break;
    case "db.query":
    case "db.exec":
      s = pick("query");
      break;
    case "http.request":
      s = `${pick("method") || "GET"} ${pick("url")}`;
      break;
    case "telegram.api":
      s = pick("method");
      break;
    default:
      s = pick("text", "caption", "key", "duration", "items", "message", "file", "type", "method");
  }
  s = s.replace(/<[^>]+>/g, "").replace(/\s+/g, " ").trim();
  return s.length > 70 ? s.slice(0, 68) + "…" : s;
}
