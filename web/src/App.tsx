import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  addEdge,
  useEdgesState,
  useNodesInitialized,
  useNodesState,
  useReactFlow,
  type Connection,
  type Edge,
  type IsValidConnection,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { api, download } from "./api";
import FlowNodeView from "./components/FlowNode";
import Inspector from "./components/Inspector";
import Palette, { DND_TYPE } from "./components/Palette";
import SettingsDialog from "./components/SettingsDialog";
import TestChat from "./components/TestChat";
import { CatalogContext, type CatalogState } from "./context";
import { edgeFor, fromWorkflow, isTrigger, layout, metaFor, toWorkflow, uniqueId, type FlowNode, type WorkflowRest } from "./convert";
import { categories, type Issue, type Json, type Workflow } from "./types";

const DRAFT_KEY = "tgcreator.draft.v1";
const nodeTypes = { tg: FlowNodeView };

const starter: Workflow = {
  name: "ربات جدید",
  version: 1,
  bot: { token: "${BOT_TOKEN}", parse_mode: "HTML" },
  nodes: [
    { id: "start", type: "trigger.command", params: { commands: ["start"] }, position: [0, 0] },
    { id: "hello", type: "telegram.send_message", params: { text: "سلام {{ escapeHTML(from.first_name) }} 👋" }, position: [320, 0] },
  ],
  connections: { start: { main: ["hello"] } },
};

function loadDraft(): Workflow {
  try {
    const raw = localStorage.getItem(DRAFT_KEY);
    if (raw) return JSON.parse(raw) as Workflow;
  } catch {
    /* ignore */
  }
  return starter;
}

export default function App() {
  const [catalog, setCatalog] = useState<CatalogState | null>(null);
  const [loadErr, setLoadErr] = useState("");
  useEffect(() => {
    Promise.all([api.catalog(), api.botMethods()])
      .then(([c, m]) =>
        setCatalog({
          metas: Object.fromEntries(c.nodes.map((n) => [n.name, n])),
          methods: m.methods,
          methodMap: Object.fromEntries(m.methods.map((x) => [x.name, x])),
          botApi: c.bot_api,
        }),
      )
      .catch((e) => setLoadErr(String(e.message ?? e)));
  }, []);
  if (loadErr) return <div className="splash">اتصال به سرور پنل ناموفق بود: {loadErr}</div>;
  if (!catalog) return <div className="splash">در حال بارگذاری…</div>;
  return (
    <CatalogContext.Provider value={catalog}>
      <ReactFlowProvider>
        <Editor />
      </ReactFlowProvider>
    </CatalogContext.Provider>
  );
}

function Editor() {
  const catalog = useContext(CatalogContext);
  const initial = useMemo(() => fromWorkflow(loadDraft()), []);
  const [rest, setRest] = useState<WorkflowRest>(initial.rest);
  const [nodes, setNodes, onNodesChange] = useNodesState<FlowNode>(initial.nodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>(initial.edges);
  const edgesRef = useRef(edges);
  edgesRef.current = edges;
  const [selected, setSelected] = useState<string | null>(null);
  const [issues, setIssues] = useState<Issue[]>([]);
  const [info, setInfo] = useState<{ updates?: string[]; requirements?: string[] }>({});
  const [showSettings, setShowSettings] = useState(false);
  const [showIssues, setShowIssues] = useState(false);
  const [testing, setTesting] = useState(false);
  const [menu, setMenu] = useState<"" | "examples" | "export">("");
  const [examples, setExamples] = useState<{ id: string; name: string; nodes: number }[]>([]);
  const [notice, setNotice] = useState("");
  const { screenToFlowPosition, fitView, setCenter, getNode, getNodes } = useReactFlow<FlowNode>();
  // Layout needs real node sizes: run it once React Flow has measured them.
  const pendingLayout = useRef(initial.needsLayout);
  const measured = useNodesInitialized();
  useEffect(() => {
    if (!measured || !pendingLayout.current) return;
    pendingLayout.current = false;
    setNodes(layout(getNodes(), edgesRef.current));
    setTimeout(() => fitView({ padding: 0.12, duration: 250 }), 30);
  }, [measured, getNodes, setNodes, fitView]);
  const canvasRef = useRef<HTMLDivElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const current = useCallback(() => toWorkflow(rest, nodes, edges), [rest, nodes, edges]);
  const currentRef = useRef(current);
  currentRef.current = current;
  const getWorkflow = useCallback(() => currentRef.current(), []);

  // ---- history, autosave, validation ----
  const history = useRef<{ past: string[]; future: string[]; last: string }>({ past: [], future: [], last: "" });
  const applying = useRef(false);
  useEffect(() => {
    const t = setTimeout(async () => {
      const wf = current();
      const json = JSON.stringify(wf);
      try {
        localStorage.setItem(DRAFT_KEY, json);
      } catch {
        /* storage full or blocked */
      }
      const h = history.current;
      if (!applying.current && h.last && json !== h.last) {
        h.past.push(h.last);
        if (h.past.length > 100) h.past.shift();
        h.future = [];
      }
      applying.current = false;
      h.last = json;
      try {
        const r = await api.validate(wf);
        setIssues(r.issues ?? []);
        setInfo({ updates: r.updates, requirements: r.requirements });
      } catch {
        /* server offline */
      }
    }, 450);
    return () => clearTimeout(t);
  }, [current]);

  const load = useCallback(
    (wf: Workflow, fit = true) => {
      const f = fromWorkflow(wf);
      setRest(f.rest);
      pendingLayout.current = f.needsLayout;
      setNodes(f.needsLayout ? layout(f.nodes, f.edges) : f.nodes);
      setEdges(f.edges);
      setSelected(null);
      if (fit) setTimeout(() => fitView({ padding: 0.12, duration: 300 }), 60);
    },
    [setNodes, setEdges, fitView],
  );

  const undo = useCallback(
    (redo = false) => {
      const h = history.current;
      const from = redo ? h.future : h.past;
      const to = redo ? h.past : h.future;
      const snap = from.pop();
      if (!snap) return;
      to.push(h.last);
      h.last = snap;
      applying.current = true;
      load(JSON.parse(snap), false);
    },
    [load],
  );

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const typing = (e.target as HTMLElement).closest("input, textarea, select");
      if (typing) return;
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "z") {
        e.preventDefault();
        undo(e.shiftKey);
      } else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "y") {
        e.preventDefault();
        undo(true);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [undo]);

  const flash = (msg: string) => {
    setNotice(msg);
    setTimeout(() => setNotice(""), 2500);
  };

  // ---- graph editing ----
  const ids = useMemo(() => nodes.map((n) => n.id), [nodes]);

  const addNode = useCallback(
    (type: string, at?: { x: number; y: number }) => {
      const { metas, methodMap } = catalog;
      const meta = metaFor(metas, type);
      const params: Record<string, Json> = {};
      for (const p of meta?.params ?? []) if (p.default !== undefined && p.type !== "json") params[p.name] = p.default;
      if (type.startsWith("tg.")) {
        for (const f of methodMap[type.slice(3)]?.fields ?? []) if (f.required && f.name === "chat_id") params.chat_id = "{{ chat.id }}";
      }
      const id = uniqueId(type, new Set(ids));
      let pos = at;
      if (!pos) {
        const r = canvasRef.current?.getBoundingClientRect();
        pos = screenToFlowPosition({ x: (r?.left ?? 0) + (r?.width ?? 800) / 2, y: (r?.top ?? 0) + (r?.height ?? 600) / 2 });
        pos = { x: pos.x - 110 + Math.random() * 40, y: pos.y - 40 + Math.random() * 40 };
      }
      setNodes((ns) => [...ns.map((n) => ({ ...n, selected: false })), { id, type: "tg", position: pos!, selected: true, data: { spec: { id, type, params } } }]);
      setSelected(id);
    },
    [catalog, ids, screenToFlowPosition, setNodes],
  );

  const onConnect = useCallback(
    (c: Connection) => {
      if (!c.source || !c.target) return;
      setEdges((es) => {
        const e = edgeFor(c.source!, c.sourceHandle ?? "main", c.target!);
        return es.some((x) => x.id === e.id) ? es : addEdge(e, es);
      });
    },
    [setEdges],
  );

  const isValidConnection: IsValidConnection = useCallback(
    (c) => c.source !== c.target && c.targetHandle === "in" && !isTrigger(getNode(c.target ?? "")?.data.spec.type ?? ""),
    [getNode],
  );

  const onDrop = useCallback(
    (e: React.DragEvent) => {
      const type = e.dataTransfer.getData(DND_TYPE);
      if (!type) return;
      e.preventDefault();
      const p = screenToFlowPosition({ x: e.clientX, y: e.clientY });
      addNode(type, { x: p.x - 110, y: p.y - 30 });
    },
    [screenToFlowPosition, addNode],
  );

  const updateSpec = useCallback(
    (id: string, spec: FlowNode["data"]["spec"]) => setNodes((ns) => ns.map((n) => (n.id === id ? { ...n, data: { ...n.data, spec } } : n))),
    [setNodes],
  );

  const rename = useCallback(
    (from: string, to: string) => {
      setNodes((ns) => ns.map((n) => (n.id === from ? { ...n, id: to, data: { ...n.data, spec: { ...n.data.spec, id: to } } } : n)));
      setEdges((es) =>
        es.map((e) => (e.source === from || e.target === from ? edgeFor(e.source === from ? to : e.source, e.sourceHandle ?? "main", e.target === from ? to : e.target) : e)),
      );
      setSelected(to);
    },
    [setNodes, setEdges],
  );

  const remove = useCallback(
    (id: string) => {
      setNodes((ns) => ns.filter((n) => n.id !== id));
      setEdges((es) => es.filter((e) => e.source !== id && e.target !== id));
      setSelected(null);
    },
    [setNodes, setEdges],
  );

  const duplicate = useCallback(
    (id: string) => {
      const n = nodes.find((x) => x.id === id);
      if (!n) return;
      const nid = uniqueId(n.data.spec.type, new Set(ids));
      setNodes((ns) => [
        ...ns.map((x) => ({ ...x, selected: false })),
        { ...n, id: nid, selected: true, position: { x: n.position.x + 40, y: n.position.y + 60 }, data: { spec: { ...structuredClone(n.data.spec), id: nid } } },
      ]);
      setSelected(nid);
    },
    [nodes, ids, setNodes],
  );

  // ---- issues on nodes ----
  const issuesByNode = useMemo(() => {
    const m: Record<string, Issue[]> = {};
    for (const i of issues) if (i.node) (m[i.node] ??= []).push(i);
    return m;
  }, [issues]);
  const displayNodes = useMemo(
    () =>
      nodes.map((n) => {
        const list = issuesByNode[n.id];
        if (!list) return n.data.issue ? { ...n, data: { ...n.data, issue: undefined, issueText: undefined } } : n;
        const level = list.some((i) => i.level === "error") ? "error" : "warning";
        return { ...n, data: { ...n.data, issue: level as "error" | "warning", issueText: list.map((i) => i.message).join("\n") } };
      }),
    [nodes, issuesByNode],
  );
  const errors = issues.filter((i) => i.level === "error").length;
  const warnings = issues.length - errors;

  const focusNode = (id: string) => {
    const n = getNode(id);
    if (!n) return;
    setSelected(id);
    setNodes((ns) => ns.map((x) => ({ ...x, selected: x.id === id })));
    setCenter(n.position.x + 110, n.position.y + 50, { zoom: 1.1, duration: 400 });
  };

  // ---- file actions ----
  const openFile = async (f: File) => {
    try {
      const wf = JSON.parse(await f.text()) as Workflow;
      if (!Array.isArray(wf.nodes)) throw new Error("فیلد nodes پیدا نشد");
      load(wf);
      flash(`«${wf.name ?? f.name}» باز شد`);
    } catch (e) {
      alert("فایل نامعتبر است: " + (e as Error).message);
    }
  };
  const exportWorkflow = () => download("workflow.json", JSON.stringify(current(), null, 2));
  const exportCompose = async () => {
    try {
      const wf = current();
      const assets = JSON.stringify(wf).includes("file:///app/assets");
      download("docker-compose.yml", await api.compose(wf, assets), "text/yaml");
    } catch (e) {
      alert("ساخت docker-compose ناموفق بود: " + (e as Error).message);
    }
  };

  const selectedNode = selected ? nodes.find((n) => n.id === selected) : undefined;
  const variables = Object.keys(rest.variables ?? {});

  return (
    <div className="app">
        <header className="topbar">
          <div className="brand">🤖 TgCreator</div>
          <input className="wf-name" dir="auto" value={rest.name} onChange={(e) => setRest({ ...rest, name: e.target.value })} aria-label="نام workflow" />
          <div className="topbar-actions">
            <button type="button" className="btn-ghost" onClick={() => confirm("workflow فعلی پاک شود؟") && load(starter)}>
              جدید
            </button>
            <button type="button" className="btn-ghost" onClick={() => fileRef.current?.click()}>
              باز کردن
            </button>
            <input ref={fileRef} type="file" accept=".json,application/json" hidden onChange={(e) => e.target.files?.[0] && openFile(e.target.files[0])} />
            <div className="menu-wrap">
              <button
                type="button"
                className="btn-ghost"
                onClick={async () => {
                  setMenu(menu === "examples" ? "" : "examples");
                  if (!examples.length) setExamples(await api.examples());
                }}
              >
                نمونه‌ها ▾
              </button>
              {menu === "examples" && (
                <div className="menu" onMouseLeave={() => setMenu("")}>
                  {examples.map((x) => (
                    <button
                      type="button"
                      key={x.id}
                      onClick={async () => {
                        setMenu("");
                        load(await api.example(x.id));
                        flash(`نمونهٔ «${x.name}» باز شد`);
                      }}
                    >
                      {x.name} <span className="muted">({x.nodes} نود)</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
            <button type="button" className="btn-ghost" onClick={() => setShowSettings(true)}>
              ⚙️ تنظیمات
            </button>
            <button
              type="button"
              className="btn-ghost"
              onClick={() => {
                setNodes(layout(getNodes(), edges));
                setTimeout(() => fitView({ padding: 0.12, duration: 300 }), 30);
              }}
            >
              مرتب‌سازی
            </button>
            <button type="button" className="btn-ghost" onClick={() => undo()} title="Ctrl+Z">
              ↶
            </button>
            <button type="button" className="btn-ghost" onClick={() => undo(true)} title="Ctrl+Shift+Z">
              ↷
            </button>
            <button type="button" className={`status-pill ${errors ? "bad" : warnings ? "warn" : "good"}`} onClick={() => setShowIssues(!showIssues)}>
              {errors ? `⛔ ${errors} خطا` : warnings ? `⚠️ ${warnings} هشدار` : "✅ بدون خطا"}
            </button>
            <div className="menu-wrap">
              <button type="button" className="btn-ghost" onClick={() => setMenu(menu === "export" ? "" : "export")}>
                خروجی ▾
              </button>
              {menu === "export" && (
                <div className="menu" onMouseLeave={() => setMenu("")}>
                  <button type="button" onClick={() => (setMenu(""), exportWorkflow())}>
                    workflow.json
                  </button>
                  <button type="button" onClick={() => (setMenu(""), exportCompose())} disabled={errors > 0}>
                    docker-compose.yml
                  </button>
                </div>
              )}
            </div>
            <button type="button" className="btn" onClick={() => setTesting(true)} disabled={errors > 0} title={errors ? "اول خطاها را برطرف کنید" : "اجرای ربات در تلگرام شبیه‌سازی‌شده"}>
              ▶ تست
            </button>
          </div>
        </header>

        <div className="main">
          <aside className="side side-palette">
            <Palette onAdd={(t) => addNode(t)} />
          </aside>

          <div className="canvas" ref={canvasRef} dir="ltr" onDragOver={(e) => (e.preventDefault(), (e.dataTransfer.dropEffect = "move"))} onDrop={onDrop}>
            <ReactFlow
              nodes={displayNodes}
              edges={edges}
              nodeTypes={nodeTypes}
              onNodesChange={onNodesChange}
              onEdgesChange={onEdgesChange}
              onConnect={onConnect}
              isValidConnection={isValidConnection}
              onSelectionChange={({ nodes: sel }) => setSelected(sel.length === 1 ? sel[0].id : null)}
              onNodesDelete={(del) => del.some((d) => d.id === selected) && setSelected(null)}
              deleteKeyCode={["Delete", "Backspace"]}
              fitView
              minZoom={0.15}
              maxZoom={2}
              defaultEdgeOptions={{ interactionWidth: 18 }}
              proOptions={{ hideAttribution: true }}
            >
              <Background gap={20} size={1.2} />
              <Controls position="bottom-left" />
              <MiniMap position="bottom-right" pannable zoomable nodeColor={(n) => categories[metaFor(catalog.metas, (n as FlowNode).data.spec.type)?.category ?? "other"]?.color ?? "#999"} />
            </ReactFlow>
            {info.updates && (
              <div className="canvas-info" dir="rtl">
                آپدیت‌ها: <code dir="ltr">{info.updates.join(", ")}</code>
                {" · "}
                سرویس‌ها: {info.requirements?.length ? <code dir="ltr">{info.requirements.join(", ")}</code> : "هیچ (فقط ران‌تایم)"}
              </div>
            )}
            {showIssues && issues.length > 0 && (
              <div className="issues-panel" dir="rtl">
                <div className="issues-head">
                  مشکلات <button type="button" className="btn-icon" onClick={() => setShowIssues(false)}>×</button>
                </div>
                {issues.map((i, k) => (
                  <button type="button" key={k} className={`issue issue-${i.level}`} onClick={() => i.node && focusNode(i.node)}>
                    {i.level === "error" ? "⛔" : "⚠️"} {i.node && <code dir="ltr">{i.node}</code>} {i.message}
                  </button>
                ))}
              </div>
            )}
            {notice && <div className="notice">{notice}</div>}
          </div>

          <aside className="side side-inspector">
            {selectedNode ? (
              <Inspector
                key={selectedNode.id}
                node={selectedNode}
                nodeIds={ids}
                variables={variables}
                issues={issuesByNode[selectedNode.id] ?? []}
                onChange={(spec) => updateSpec(selectedNode.id, spec)}
                onRename={rename}
                onDelete={() => remove(selectedNode.id)}
                onDuplicate={() => duplicate(selectedNode.id)}
              />
            ) : (
              <div className="insp-empty">
                <h3>راهنما</h3>
                <ul>
                  <li>نودها را از فهرست بکشید و روی صفحه رها کنید (یا کلیک کنید).</li>
                  <li>از دایرهٔ سمت راست هر نود به دایرهٔ سمت چپ نود بعدی بکشید تا وصل شوند.</li>
                  <li>هر نود یک خروجی «خطا» دارد؛ اگر وصلش کنید، خطاها به آن مسیر می‌روند.</li>
                  <li>برای حذف، نود یا خط را انتخاب کنید و Delete بزنید.</li>
                  <li>در متن‌ها با <code dir="ltr">{"{{ from.first_name }}"}</code> از داده‌های پیام استفاده کنید؛ دکمهٔ <code>{"{x}"}</code> فهرست متغیرها را نشان می‌دهد.</li>
                  <li>با «▶ تست» ربات را بدون توکن، در یک تلگرام شبیه‌سازی‌شده امتحان کنید.</li>
                </ul>
                <p className="muted">
                  {nodes.length} نود · {edges.length} اتصال · Ctrl+Z برای برگشت
                </p>
              </div>
            )}
          </aside>

          {testing && (
            <aside className="side side-test">
              <TestChat workflow={getWorkflow} onClose={() => setTesting(false)} />
            </aside>
          )}
        </div>

      {showSettings && <SettingsDialog value={rest} onChange={setRest} onClose={() => setShowSettings(false)} />}
    </div>
  );
}
