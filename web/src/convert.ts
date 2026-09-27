import dagre from "@dagrejs/dagre";
import type { Edge, Node } from "@xyflow/react";
import type { NodeMeta, NodeType, Workflow, WorkflowNode } from "./types";

export interface FlowData extends Record<string, unknown> {
  spec: WorkflowNode;
  issue?: "error" | "warning";
  issueText?: string;
  connected?: string[]; // outputs that have at least one edge
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
  return { id: `${source}|${output}|${target}`, type: "tg", source, sourceHandle: output, target, targetHandle: "in" };
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

/**
 * Left-to-right layout. Each independent flow (trigger + what it reaches) is
 * laid out on its own and the flows are packed in rows, so large workflows
 * stay readable instead of becoming one very tall column.
 */
export function layout(nodes: FlowNode[], edges: Edge[], aspect = 1.6): FlowNode[] {
  const size = (n: FlowNode) => ({ w: n.measured?.width ?? 250, h: n.measured?.height ?? 70 });

  // Weakly connected components (union-find).
  const parent = new Map(nodes.map((n) => [n.id, n.id]));
  const find = (x: string): string => {
    let r = x;
    while (parent.get(r) !== r) r = parent.get(r)!;
    parent.set(x, r);
    return r;
  };
  for (const e of edges) if (parent.has(e.source) && parent.has(e.target)) parent.set(find(e.source), find(e.target));
  const groups = new Map<string, FlowNode[]>();
  for (const n of nodes) {
    const r = find(n.id);
    if (!groups.has(r)) groups.set(r, []);
    groups.get(r)!.push(n);
  }

  const placed = new Map<string, { x: number; y: number }>();
  const blocks: { ids: string[]; w: number; h: number; pos: Map<string, { x: number; y: number }> }[] = [];
  for (const members of groups.values()) {
    const ids = new Set(members.map((m) => m.id));
    const g = new dagre.graphlib.Graph();
    g.setGraph({ rankdir: "LR", nodesep: 26, ranksep: 110 });
    g.setDefaultEdgeLabel(() => ({}));
    for (const n of members) g.setNode(n.id, { width: size(n).w, height: size(n).h });
    for (const e of edges) if (ids.has(e.source) && ids.has(e.target)) g.setEdge(e.source, e.target);
    dagre.layout(g);
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    const pos = new Map<string, { x: number; y: number }>();
    for (const n of members) {
      const p = g.node(n.id);
      const { w, h } = size(n);
      const x = p.x - w / 2, y = p.y - h / 2;
      pos.set(n.id, { x, y });
      minX = Math.min(minX, x); minY = Math.min(minY, y);
      maxX = Math.max(maxX, x + w); maxY = Math.max(maxY, y + h);
    }
    for (const [id, p] of pos) pos.set(id, { x: p.x - minX, y: p.y - minY });
    blocks.push({ ids: [...ids], w: maxX - minX, h: maxY - minY, pos });
  }

  // Pack blocks in rows about as wide as the layout is tall.
  const area = blocks.reduce((a, b) => a + (b.w + 80) * (b.h + 60), 0);
  const rowWidth = Math.max(1400, Math.sqrt(area * Math.min(Math.max(aspect, 0.8), 2.5)));
  let x = 0, y = 0, rowH = 0;
  for (const b of blocks) {
    if (x > 0 && x + b.w > rowWidth) {
      x = 0;
      y += rowH + 70;
      rowH = 0;
    }
    for (const id of b.ids) {
      const p = b.pos.get(id)!;
      placed.set(id, { x: x + p.x, y: y + p.y });
    }
    x += b.w + 90;
    rowH = Math.max(rowH, b.h);
  }
  return nodes.map((n) => ({ ...n, position: placed.get(n.id) ?? n.position }));
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
  s = humanize(s.replace(/<[^>]+>/g, "")).replace(/\s+/g, " ").trim();
  return s.length > 70 ? s.slice(0, 68) + "…" : s;
}

const friendly: Record<string, string> = {
  "from.first_name": "نام کاربر",
  "from.id": "شناسهٔ کاربر",
  "from.username": "یوزرنیم",
  "mention(from)": "منشن کاربر",
  text: "متن پیام",
  "chat.id": "چت فعلی",
  "chat.title": "نام گروه",
  "message.message_id": "پیام فعلی",
  args_text: "آرگومان‌ها",
  "args[0]": "آرگومان اول",
  data: "داده دکمه",
  "reply.from.id": "کاربر ریپلای‌شده",
  "bot.username": "یوزرنیم ربات",
};

/** Shows {{ expressions }} in node summaries as short readable tokens. */
function humanize(s: string): string {
  return s.replace(/\{\{\s*(.*?)\s*\}\}/g, (_, raw: string) => {
    const expr = raw.replace(/^escapeHTML\((.*)\)$/, "$1").trim();
    return `«${friendly[expr] ?? expr}»`;
  });
}
