import {
  Background,
  BackgroundVariant,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  addEdge,
  useEdgesState,
  useNodesInitialized,
  useNodesState,
  useReactFlow,
  useViewport,
  type Connection,
  type Edge,
  type FinalConnectionState,
  type IsValidConnection,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  BookOpen,
  ChevronDown,
  CircleAlert,
  CircleCheck,
  Download,
  FilePlus,
  FileJson,
  LayoutGrid,
  Map as MapIcon,
  Maximize,
  Moon,
  PanelRightClose,
  PanelRightOpen,
  Play,
  Plus,
  Redo2,
  Settings,
  Square,
  Sun,
  TriangleAlert,
  Undo2,
  Upload,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import { useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { api, download } from "./api";
import EdgeView from "./components/EdgeView";
import FlowNodeView from "./components/FlowNode";
import Inspector from "./components/Inspector";
import NodeCreator, { DND_TYPE } from "./components/NodeCreator";
import SettingsDialog from "./components/SettingsDialog";
import TestChat from "./components/TestChat";
import { CatalogContext, EditorContext, type CatalogState } from "./context";
import { edgeFor, fromWorkflow, isTrigger, layout, metaFor, outputsOf, toWorkflow, uniqueId, type FlowNode, type WorkflowRest } from "./convert";
import { ToastProvider, useToast } from "./toast";
import { categories, type Issue, type Json, type Workflow } from "./types";

const DRAFT_KEY = "tgcreator.draft.v1";
const THEME_KEY = "tgcreator.theme";
const nodeTypes = { tg: FlowNodeView };
const edgeTypes = { tg: EdgeView };
const defaultEdgeOptions = { type: "tg" };

const starter: Workflow = {
  name: "ربات جدید",
  version: 1,
  bot: { token: "${BOT_TOKEN}", parse_mode: "HTML" },
  nodes: [
    { id: "start", type: "trigger.command", params: { commands: ["start"] }, position: [0, 0] },
    { id: "hello", type: "telegram.send_message", params: { text: "سلام {{ escapeHTML(from.first_name) }} 👋" }, position: [340, 0] },
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

type Theme = "light" | "dark";
function initialTheme(): Theme {
  try {
    const t = localStorage.getItem(THEME_KEY);
    if (t === "light" || t === "dark") return t;
  } catch {
    /* ignore */
  }
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light";
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
  if (loadErr)
    return (
      <div className="splash">
        <CircleAlert size={28} />
        <div>اتصال به سرور پنل ناموفق بود</div>
        <code dir="ltr">{loadErr}</code>
      </div>
    );
  if (!catalog)
    return (
      <div className="splash">
        <span className="spinner" />
      </div>
    );
  return (
    <CatalogContext.Provider value={catalog}>
      <ToastProvider>
        <ReactFlowProvider>
          <Editor />
        </ReactFlowProvider>
      </ToastProvider>
    </CatalogContext.Provider>
  );
}

interface Picker {
  x: number; // screen position of the popover
  y: number;
  at: { x: number; y: number }; // flow position for the new node
  source?: string;
  handle?: string;
}

function Editor() {
  const catalog = useContext(CatalogContext);
  const toast = useToast();
  const initial = useMemo(() => fromWorkflow(loadDraft()), []);
  const [rest, setRest] = useState<WorkflowRest>(initial.rest);
  const [nodes, setNodes, onNodesChange] = useNodesState<FlowNode>(initial.nodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>(initial.edges);
  const edgesRef = useRef(edges);
  edgesRef.current = edges;
  const [selected, setSelected] = useState<string | null>(null);
  const [issues, setIssues] = useState<Issue[]>([]);
  const [info, setInfo] = useState<{ updates?: string[]; requirements?: string[] }>({});
  const [saved, setSaved] = useState(true);
  const [showSettings, setShowSettings] = useState(false);
  const [showIssues, setShowIssues] = useState(false);
  const [menu, setMenu] = useState(false);
  const [testing, setTesting] = useState(false);
  const [panelOpen, setPanelOpen] = useState(true);
  const [minimap, setMinimap] = useState(false);
  const [picker, setPicker] = useState<Picker | null>(null);
  const [examples, setExamples] = useState<{ id: string; name: string; nodes: number }[]>([]);
  const [theme, setTheme] = useState<Theme>(initialTheme);
  const { screenToFlowPosition, flowToScreenPosition, fitView, setCenter, getNode, getNodes, zoomIn, zoomOut } = useReactFlow<FlowNode>();
  const { zoom } = useViewport();
  const canvasRef = useRef<HTMLDivElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const aspect = () => {
    const r = canvasRef.current?.getBoundingClientRect();
    return r && r.height ? r.width / r.height : 1.6;
  };

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    try {
      localStorage.setItem(THEME_KEY, theme);
    } catch {
      /* ignore */
    }
  }, [theme]);

  // Layout needs real node sizes: run it once React Flow has measured them.
  const pendingLayout = useRef(initial.needsLayout);
  const measured = useNodesInitialized();
  useEffect(() => {
    if (!measured || !pendingLayout.current) return;
    pendingLayout.current = false;
    setNodes(layout(getNodes(), edgesRef.current, aspect()));
    setTimeout(() => fitView({ padding: 0.15, duration: 250 }), 30);
  }, [measured, getNodes, setNodes, fitView]);

  const current = useCallback(() => toWorkflow(rest, nodes, edges), [rest, nodes, edges]);
  const currentRef = useRef(current);
  currentRef.current = current;
  const getWorkflow = useCallback(() => currentRef.current(), []);

  // ---- history, autosave, validation ----
  const history = useRef<{ past: string[]; future: string[]; last: string }>({ past: [], future: [], last: "" });
  const applying = useRef(false);
  useEffect(() => {
    setSaved(false);
    const t = setTimeout(async () => {
      const wf = current();
      const json = JSON.stringify(wf);
      try {
        localStorage.setItem(DRAFT_KEY, json);
      } catch {
        /* storage full or blocked */
      }
      setSaved(true);
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
    }, 400);
    return () => clearTimeout(t);
  }, [current]);

  // Structural edits record a history step right away, so a quick
  // add-then-delete can still be undone one step at a time.
  const checkpoint = useCallback(() => {
    const h = history.current;
    const json = JSON.stringify(currentRef.current());
    if (h.last && json !== h.last) {
      h.past.push(h.last);
      if (h.past.length > 100) h.past.shift();
      h.future = [];
    }
    if (json !== h.last) h.last = json;
  }, []);

  const load = useCallback(
    (wf: Workflow, fit = true) => {
      const f = fromWorkflow(wf);
      setRest(f.rest);
      pendingLayout.current = f.needsLayout;
      setNodes(f.needsLayout ? layout(f.nodes, f.edges, aspect()) : f.nodes);
      setEdges(f.edges);
      setSelected(null);
      if (fit) setTimeout(() => fitView({ padding: 0.15, duration: 300 }), 60);
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

  // ---- graph editing ----
  const ids = useMemo(() => nodes.map((n) => n.id), [nodes]);

  const createNode = useCallback(
    (type: string, pos: { x: number; y: number }, connectFrom?: { source: string; handle: string }) => {
      const meta = metaFor(catalog.metas, type);
      const params: Record<string, Json> = {};
      for (const p of meta?.params ?? []) if (p.default !== undefined && p.type !== "json") params[p.name] = p.default;
      if (type.startsWith("tg.")) {
        for (const f of catalog.methodMap[type.slice(3)]?.fields ?? []) if (f.required && f.name === "chat_id") params.chat_id = "{{ chat.id }}";
      }
      const id = uniqueId(type, new Set(getNodes().map((n) => n.id)));
      checkpoint();
      setNodes((ns) => [...ns.map((n) => ({ ...n, selected: false })), { id, type: "tg", position: pos, selected: true, data: { spec: { id, type, params } } }]);
      if (connectFrom && !isTrigger(type)) setEdges((es) => addEdge(edgeFor(connectFrom.source, connectFrom.handle, id), es));
      setSelected(id);
    },
    [catalog, getNodes, setNodes, setEdges, checkpoint],
  );

  const openPicker = useCallback(
    (at: { x: number; y: number }, from?: { source: string; handle: string }) => {
      const s = flowToScreenPosition(at);
      const r = canvasRef.current?.getBoundingClientRect();
      const x = Math.min(Math.max(s.x, (r?.left ?? 0) + 8), (r?.right ?? window.innerWidth) - 340);
      const y = Math.min(Math.max(s.y - 40, (r?.top ?? 0) + 8), (r?.bottom ?? window.innerHeight) - 440);
      setPicker({ x, y, at, source: from?.source, handle: from?.handle });
    },
    [flowToScreenPosition],
  );

  const addAfter = useCallback(
    (source: string, handle: string) => {
      const n = getNode(source);
      if (!n) return;
      const outs = outputsOf(n.data.spec, metaFor(catalog.metas, n.data.spec.type));
      const idx = Math.max(0, outs.indexOf(handle));
      const at = { x: n.position.x + (n.measured?.width ?? 240) + 110, y: n.position.y + (idx - (outs.length - 1) / 2) * 100 };
      openPicker(at, { source, handle });
    },
    [getNode, catalog, openPicker],
  );

  const openPickerCenter = useCallback(() => {
    const r = canvasRef.current?.getBoundingClientRect();
    const at = screenToFlowPosition({ x: (r?.left ?? 0) + (r?.width ?? 800) / 2 - 120, y: (r?.top ?? 0) + (r?.height ?? 600) / 3 });
    openPicker(at);
  }, [screenToFlowPosition, openPicker]);

  const addFromPanel = useCallback(
    (type: string) => {
      const r = canvasRef.current?.getBoundingClientRect();
      const p = screenToFlowPosition({ x: (r?.left ?? 0) + (r?.width ?? 800) / 2, y: (r?.top ?? 0) + (r?.height ?? 600) / 2 });
      createNode(type, { x: p.x - 120 + Math.random() * 40, y: p.y - 30 + Math.random() * 40 });
    },
    [screenToFlowPosition, createNode],
  );

  const onConnect = useCallback(
    (c: Connection) => {
      if (!c.source || !c.target) return;
      checkpoint();
      setEdges((es) => {
        const e = edgeFor(c.source!, c.sourceHandle ?? "main", c.target!);
        return es.some((x) => x.id === e.id) ? es : addEdge(e, es);
      });
    },
    [setEdges, checkpoint],
  );

  // Dropping a connection on empty canvas opens the picker (like n8n).
  const onConnectEnd = useCallback(
    (e: MouseEvent | TouchEvent, st: FinalConnectionState) => {
      if (st.isValid || !st.fromNode || st.fromHandle?.type !== "source") return;
      const pt = "changedTouches" in e ? e.changedTouches[0] : e;
      const at = screenToFlowPosition({ x: pt.clientX, y: pt.clientY });
      openPicker({ x: at.x, y: at.y - 30 }, { source: st.fromNode.id, handle: st.fromHandle.id ?? "main" });
    },
    [screenToFlowPosition, openPicker],
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
      createNode(type, { x: p.x - 120, y: p.y - 32 });
    },
    [screenToFlowPosition, createNode],
  );

  const updateSpec = useCallback((id: string, spec: FlowNode["data"]["spec"]) => setNodes((ns) => ns.map((n) => (n.id === id ? { ...n, data: { ...n.data, spec } } : n))), [setNodes]);

  const rename = useCallback(
    (from: string, to: string) => {
      checkpoint();
      setNodes((ns) => ns.map((n) => (n.id === from ? { ...n, id: to, data: { ...n.data, spec: { ...n.data.spec, id: to } } } : n)));
      setEdges((es) => es.map((e) => (e.source === from || e.target === from ? edgeFor(e.source === from ? to : e.source, e.sourceHandle ?? "main", e.target === from ? to : e.target) : e)));
      setSelected(to);
    },
    [setNodes, setEdges, checkpoint],
  );

  const remove = useCallback(
    (id: string) => {
      checkpoint();
      setNodes((ns) => ns.filter((n) => n.id !== id));
      setEdges((es) => es.filter((e) => e.source !== id && e.target !== id));
      setSelected(null);
    },
    [setNodes, setEdges, checkpoint],
  );

  const duplicate = useCallback(
    (id: string) => {
      const n = nodes.find((x) => x.id === id);
      if (!n) return;
      const nid = uniqueId(n.data.spec.type, new Set(ids));
      checkpoint();
      setNodes((ns) => [...ns.map((x) => ({ ...x, selected: false })), { ...n, id: nid, selected: true, position: { x: n.position.x + 40, y: n.position.y + 90 }, data: { spec: { ...structuredClone(n.data.spec), id: nid } } }]);
      setSelected(nid);
    },
    [nodes, ids, setNodes, checkpoint],
  );

  // ---- keyboard ----
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target instanceof Element ? e.target : null;
      const typing = target?.closest("input, textarea, select, [contenteditable]");
      const mod = e.ctrlKey || e.metaKey;
      if (mod && e.key.toLowerCase() === "k") {
        e.preventDefault();
        openPickerCenter();
        return;
      }
      if (typing) return;
      if (mod && e.key.toLowerCase() === "z") {
        e.preventDefault();
        undo(e.shiftKey);
      } else if (mod && e.key.toLowerCase() === "y") {
        e.preventDefault();
        undo(true);
      } else if (e.key === "Tab") {
        e.preventDefault();
        openPickerCenter();
      } else if (e.key === "Escape") {
        setPicker(null);
        setSelected(null);
        setNodes((ns) => ns.map((n) => (n.selected ? { ...n, selected: false } : n)));
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [undo, openPickerCenter, setNodes]);

  // ---- decorate nodes with issues and connected outputs ----
  const issuesByNode = useMemo(() => {
    const m: Record<string, Issue[]> = {};
    for (const i of issues) if (i.node) (m[i.node] ??= []).push(i);
    return m;
  }, [issues]);
  const connectedBy = useMemo(() => {
    const m: Record<string, string[]> = {};
    for (const e of edges) (m[e.source] ??= []).push(e.sourceHandle ?? "main");
    return m;
  }, [edges]);
  const displayNodes = useMemo(
    () =>
      nodes.map((n) => {
        const list = issuesByNode[n.id];
        const level = list ? (list.some((i) => i.level === "error") ? "error" : "warning") : undefined;
        return { ...n, data: { ...n.data, issue: level as "error" | "warning" | undefined, issueText: list?.map((i) => i.message).join("\n"), connected: connectedBy[n.id] } };
      }),
    [nodes, issuesByNode, connectedBy],
  );
  const errors = issues.filter((i) => i.level === "error").length;
  const warnings = issues.length - errors;

  const focusNode = (id: string) => {
    const n = getNode(id);
    if (!n) return;
    setSelected(id);
    setNodes((ns) => ns.map((x) => ({ ...x, selected: x.id === id })));
    setCenter(n.position.x + 120, n.position.y + 40, { zoom: 1.1, duration: 400 });
  };

  // ---- file actions ----
  const openFile = async (f: File) => {
    try {
      const wf = JSON.parse(await f.text()) as Workflow;
      if (!Array.isArray(wf.nodes)) throw new Error("فیلد nodes پیدا نشد");
      load(wf);
      toast(`«${wf.name ?? f.name}» باز شد`, "success");
    } catch (e) {
      toast("فایل نامعتبر است: " + (e as Error).message, "error");
    }
  };
  const exportCompose = async () => {
    try {
      const wf = current();
      download("docker-compose.yml", await api.compose(wf, JSON.stringify(wf).includes("file:///app/assets")), "text/yaml");
    } catch (e) {
      toast("ساخت docker-compose ناموفق بود: " + (e as Error).message, "error");
    }
  };

  const selectedNode = selected ? nodes.find((n) => n.id === selected) : undefined;
  const variables = Object.keys(rest.variables ?? {});

  return (
    <EditorContext.Provider value={{ addAfter }}>
      <div className="app">
        <header className="hdr">
          <div className="brand">
            <span className="brand-mark">
              <Send2 />
            </span>
            TgCreator
          </div>
          <div className="doc">
            <input className="doc-name" dir="auto" value={rest.name} onChange={(e) => setRest({ ...rest, name: e.target.value })} aria-label="نام workflow" />
            <span className={`doc-saved${saved ? "" : " busy"}`}>{saved ? "ذخیره شد" : "در حال ذخیره…"}</span>
          </div>

          <div className="hdr-actions">
            <div className="btn-group">
              <button type="button" className="icon-btn" onClick={() => undo()} title="برگشت (Ctrl+Z)">
                <Undo2 size={16} />
              </button>
              <button type="button" className="icon-btn" onClick={() => undo(true)} title="دوباره (Ctrl+Shift+Z)">
                <Redo2 size={16} />
              </button>
            </div>

            <div className="menu-wrap">
              <button type="button" className={`status ${errors ? "bad" : warnings ? "warn" : "good"}`} onClick={() => setShowIssues(!showIssues)}>
                {errors ? <CircleAlert size={15} /> : warnings ? <TriangleAlert size={15} /> : <CircleCheck size={15} />}
                {errors ? `${errors} خطا` : warnings ? `${warnings} هشدار` : "آماده"}
              </button>
              {showIssues && (
                <div className="pop issues-pop" onMouseLeave={() => setShowIssues(false)}>
                  <div className="pop-title">وضعیت workflow</div>
                  <div className="kv-info">
                    <span>آپدیت‌های دریافتی</span>
                    <code dir="ltr">{info.updates?.join(", ") || "—"}</code>
                    <span>سرویس‌ها</span>
                    <code dir="ltr">{info.requirements?.length ? info.requirements.join(", ") : "فقط ران‌تایم"}</code>
                  </div>
                  {issues.length === 0 && <div className="ok-line">✓ مشکلی پیدا نشد</div>}
                  {issues.map((i, k) => (
                    <button type="button" key={k} className={`issue-line ${i.level}`} onClick={() => i.node && focusNode(i.node)}>
                      {i.level === "error" ? <CircleAlert size={14} /> : <TriangleAlert size={14} />}
                      <span>
                        {i.node && <code dir="ltr">{i.node}</code>} {i.message}
                      </span>
                    </button>
                  ))}
                </div>
              )}
            </div>

            <div className="menu-wrap">
              <button
                type="button"
                className="btn btn-ghost"
                onClick={async () => {
                  setMenu(!menu);
                  if (!examples.length) setExamples(await api.examples().catch(() => []));
                }}
              >
                <FileJson size={16} /> فایل <ChevronDown size={14} />
              </button>
              {menu && (
                <div className="pop file-menu" onMouseLeave={() => setMenu(false)}>
                  <button type="button" className="pop-item" onClick={() => (setMenu(false), confirm("workflow فعلی پاک شود؟") && load(starter))}>
                    <FilePlus size={15} /> workflow جدید
                  </button>
                  <button type="button" className="pop-item" onClick={() => (setMenu(false), fileRef.current?.click())}>
                    <Upload size={15} /> باز کردن فایل…
                  </button>
                  <div className="pop-sep" />
                  <div className="pop-title">
                    <BookOpen size={13} /> نمونه‌ها
                  </div>
                  {examples.map((x) => (
                    <button
                      type="button"
                      key={x.id}
                      className="pop-item"
                      onClick={async () => {
                        setMenu(false);
                        load(await api.example(x.id));
                        toast(`نمونهٔ «${x.name}» باز شد`, "success");
                      }}
                    >
                      <span>{x.name}</span>
                      <span className="muted">{x.nodes} نود</span>
                    </button>
                  ))}
                  <div className="pop-sep" />
                  <button type="button" className="pop-item" onClick={() => (setMenu(false), download("workflow.json", JSON.stringify(current(), null, 2)))}>
                    <Download size={15} /> دانلود workflow.json
                  </button>
                  <button type="button" className="pop-item" disabled={errors > 0} onClick={() => (setMenu(false), exportCompose())}>
                    <Download size={15} /> دانلود docker-compose.yml
                  </button>
                </div>
              )}
            </div>
            <input ref={fileRef} type="file" accept=".json,application/json" hidden onChange={(e) => e.target.files?.[0] && openFile(e.target.files[0])} />

            <button type="button" className="icon-btn" onClick={() => setShowSettings(true)} title="تنظیمات workflow">
              <Settings size={17} />
            </button>
            <button type="button" className="icon-btn" onClick={() => setTheme(theme === "dark" ? "light" : "dark")} title="تم روشن/تیره">
              {theme === "dark" ? <Sun size={17} /> : <Moon size={17} />}
            </button>
            <button
              type="button"
              className={`btn ${testing ? "btn-stop" : "btn-primary"}`}
              onClick={() => setTesting(!testing)}
              disabled={!testing && errors > 0}
              title={errors ? "اول خطاها را برطرف کنید" : "اجرای ربات در تلگرام شبیه‌سازی‌شده"}
            >
              {testing ? <Square size={14} fill="currentColor" /> : <Play size={14} fill="currentColor" />}
              {testing ? "توقف تست" : "تست ربات"}
            </button>
          </div>
        </header>

        <div className="workspace">
          <aside className={`nodes-panel${panelOpen ? "" : " closed"}`}>
            {panelOpen ? (
              <>
                <div className="panel-head">
                  <span>نودها</span>
                  <button type="button" className="icon-btn" onClick={() => setPanelOpen(false)} title="بستن پنل">
                    <PanelRightClose size={16} />
                  </button>
                </div>
                <NodeCreator variant="panel" onPick={addFromPanel} />
              </>
            ) : (
              <button type="button" className="rail-btn" onClick={() => setPanelOpen(true)} title="نمایش نودها">
                <PanelRightOpen size={17} />
              </button>
            )}
          </aside>

          <main className="canvas" ref={canvasRef} dir="ltr" onDragOver={(e) => (e.preventDefault(), (e.dataTransfer.dropEffect = "move"))} onDrop={onDrop}>
            <ReactFlow
              nodes={displayNodes}
              edges={edges}
              nodeTypes={nodeTypes}
              edgeTypes={edgeTypes}
              defaultEdgeOptions={defaultEdgeOptions}
              onNodesChange={(ch) => {
                if (ch.some((c) => c.type === "remove")) checkpoint();
                onNodesChange(ch);
              }}
              onEdgesChange={(ch) => {
                if (ch.some((c) => c.type === "remove")) checkpoint();
                onEdgesChange(ch);
              }}
              onConnect={onConnect}
              onConnectEnd={onConnectEnd}
              isValidConnection={isValidConnection}
              onSelectionChange={({ nodes: sel }) => setSelected(sel.length === 1 ? sel[0].id : null)}
              onPaneClick={() => setPicker(null)}
              deleteKeyCode={["Delete", "Backspace"]}
              connectionLineStyle={{ stroke: "var(--accent)", strokeWidth: 2 }}
              fitView
              fitViewOptions={{ padding: 0.2, maxZoom: 1.1 }}
              minZoom={0.15}
              maxZoom={2}
              proOptions={{ hideAttribution: true }}
            >
              <Background variant={BackgroundVariant.Dots} gap={22} size={1.4} />
              {minimap && <MiniMap position="bottom-left" pannable zoomable nodeBorderRadius={8} nodeColor={(n) => categories[metaFor(catalog.metas, (n as FlowNode).data.spec.type)?.category ?? "other"]?.color ?? "#999"} />}
            </ReactFlow>

            <div className="toolbar" dir="rtl">
              <button type="button" className="tb-add" onClick={openPickerCenter} title="افزودن نود (Tab یا Ctrl+K)">
                <Plus size={16} /> افزودن نود
              </button>
              <span className="tb-sep" />
              <button type="button" className="icon-btn" onClick={() => zoomOut({ duration: 150 })} title="کوچک‌نمایی">
                <ZoomOut size={16} />
              </button>
              <span className="tb-zoom">{Math.round(zoom * 100)}٪</span>
              <button type="button" className="icon-btn" onClick={() => zoomIn({ duration: 150 })} title="بزرگ‌نمایی">
                <ZoomIn size={16} />
              </button>
              <button type="button" className="icon-btn" onClick={() => fitView({ padding: 0.15, duration: 250 })} title="نمایش همه">
                <Maximize size={16} />
              </button>
              <span className="tb-sep" />
              <button
                type="button"
                className="icon-btn"
                title="مرتب‌سازی خودکار"
                onClick={() => {
                  setNodes(layout(getNodes(), edges, aspect()));
                  setTimeout(() => fitView({ padding: 0.15, duration: 300 }), 30);
                }}
              >
                <LayoutGrid size={16} />
              </button>
              <button type="button" className={`icon-btn${minimap ? " on" : ""}`} title="نقشهٔ کوچک" onClick={() => setMinimap(!minimap)}>
                <MapIcon size={16} />
              </button>
            </div>

            {nodes.length === 0 && (
              <div className="empty" dir="rtl">
                <div className="empty-card">
                  <div className="empty-icon">
                    <Send2 />
                  </div>
                  <h3>ربات خود را بسازید</h3>
                  <p>با یک شروع‌کننده (مثلاً دستور /start) آغاز کنید و بعد با «+» نودهای بعدی را اضافه کنید.</p>
                  <div className="empty-actions">
                    <button type="button" className="btn btn-primary" onClick={openPickerCenter}>
                      <Plus size={15} /> افزودن اولین نود
                    </button>
                    <button type="button" className="btn btn-ghost" onClick={() => load(starter)}>
                      شروع از نمونهٔ ساده
                    </button>
                  </div>
                </div>
              </div>
            )}

            {selectedNode && (
              <div className="insp-float" dir="rtl">
                <Inspector
                  key={selectedNode.id}
                  node={selectedNode}
                  nodeIds={ids}
                  variables={variables}
                  issues={issuesByNode[selectedNode.id] ?? []}
                  description={catalog.metas[selectedNode.data.spec.type]?.description ?? ""}
                  onChange={(spec) => updateSpec(selectedNode.id, spec)}
                  onRename={rename}
                  onDelete={() => remove(selectedNode.id)}
                  onDuplicate={() => duplicate(selectedNode.id)}
                  onClose={() => {
                    setSelected(null);
                    setNodes((ns) => ns.map((n) => (n.selected ? { ...n, selected: false } : n)));
                  }}
                />
              </div>
            )}
          </main>

          {testing && (
            <aside className="test-panel">
              <TestChat workflow={getWorkflow} onClose={() => setTesting(false)} />
            </aside>
          )}
        </div>

        {picker && (
          <div className="picker" style={{ left: picker.x, top: picker.y }} dir="rtl">
            <NodeCreator
              variant="popover"
              autoFocus
              noTriggers={!!picker.source}
              onClose={() => setPicker(null)}
              onPick={(type) => {
                createNode(type, picker.at, picker.source ? { source: picker.source, handle: picker.handle ?? "main" } : undefined);
                setPicker(null);
              }}
            />
          </div>
        )}

        {showSettings && <SettingsDialog value={rest} onChange={setRest} onClose={() => setShowSettings(false)} />}
      </div>
    </EditorContext.Provider>
  );
}

/** Paper-plane logo mark. */
function Send2() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden>
      <path d="M21.5 3.5 2.9 10.6c-1 .4-1 1.8.1 2.1l4.6 1.4 1.8 5.6c.3.9 1.4 1.1 2 .4l2.6-2.6 4.6 3.4c.8.6 1.9.2 2.1-.8L23 4.9c.2-1-.7-1.8-1.5-1.4Z" fill="currentColor" opacity=".9" />
      <path d="m8 14 9.5-7.2L10 15.5" fill="none" stroke="var(--brand-ink)" strokeWidth="1.4" strokeLinecap="round" />
    </svg>
  );
}
