import { ChevronDown, Plug, Search, X } from "lucide-react";
import { useContext, useEffect, useMemo, useRef, useState } from "react";
import { CatalogContext } from "../context";
import { NodeIcon } from "../icons";
import { categories } from "../types";

export const DND_TYPE = "application/x-tgcreator-node";
const order = ["trigger", "telegram", "logic", "state", "redis", "db", "http"];

interface Item {
  type: string;
  label: string;
  summary: string;
  icon?: string;
  cat: string;
}

interface Props {
  variant: "panel" | "popover";
  onPick: (type: string) => void;
  onClose?: () => void;
  /** Hide triggers (e.g. when adding after an output). */
  noTriggers?: boolean;
  autoFocus?: boolean;
}

export default function NodeCreator({ variant, onPick, onClose, noTriggers, autoFocus }: Props) {
  const { metas, methods, botApi } = useContext(CatalogContext);
  const [q, setQ] = useState("");
  const [open, setOpen] = useState<Record<string, boolean>>({ trigger: true, telegram: true, logic: true });
  const [cursor, setCursor] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const query = q.trim().toLowerCase();

  useEffect(() => {
    if (autoFocus) inputRef.current?.focus();
  }, [autoFocus]);

  const items: Item[] = useMemo(
    () =>
      Object.values(metas)
        .filter((n) => n.name !== "tg." && !(noTriggers && n.meta.category === "trigger"))
        .map((n) => ({ type: n.name, label: n.meta.label, summary: n.meta.summary, icon: n.meta.icon, cat: n.meta.category })),
    [metas, noTriggers],
  );

  const hits = useMemo(() => {
    if (!query) return [];
    const found = items.filter((i) => `${i.label} ${i.summary} ${i.type}`.toLowerCase().includes(query));
    const apis = methods
      .filter((m) => m.name.toLowerCase().includes(query))
      .slice(0, 30)
      .map((m) => ({ type: "tg." + m.name, label: m.name, summary: "متد Bot API", icon: "plug", cat: "telegram" }));
    return [...found, ...apis];
  }, [items, methods, query]);

  useEffect(() => setCursor(0), [query]);
  useEffect(() => {
    listRef.current?.querySelector(".nc-item.active")?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") onClose?.();
    if (!hits.length) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setCursor((c) => Math.min(c + 1, hits.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setCursor((c) => Math.max(c - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      onPick(hits[cursor].type);
    }
  };

  const row = (it: Item, active = false) => (
    <button
      type="button"
      key={it.type}
      className={`nc-item${active ? " active" : ""}`}
      draggable={variant === "panel"}
      onDragStart={(e) => {
        e.dataTransfer.setData(DND_TYPE, it.type);
        e.dataTransfer.effectAllowed = "move";
      }}
      onClick={() => onPick(it.type)}
      style={{ ["--cat" as string]: categories[it.cat]?.color }}
    >
      <span className="nc-icon">
        <NodeIcon name={it.icon} size={16} />
      </span>
      <span className="nc-text">
        <span className="nc-label" dir="auto">
          {it.label}
        </span>
        <span className="nc-sum">{it.summary}</span>
      </span>
    </button>
  );

  return (
    <div className={`nc nc-${variant}`} onKeyDown={onKey}>
      <div className="nc-search">
        <Search size={15} className="nc-search-icon" />
        <input ref={inputRef} className="nc-input" placeholder="جست‌وجوی نود یا متد تلگرام…" value={q} onChange={(e) => setQ(e.target.value)} />
        {variant === "popover" && (
          <button type="button" className="icon-btn" onClick={onClose} aria-label="بستن">
            <X size={15} />
          </button>
        )}
      </div>
      <div className="nc-list" ref={listRef}>
        {query ? (
          hits.length ? (
            hits.map((it, i) => row(it, i === cursor))
          ) : (
            <div className="nc-empty">چیزی پیدا نشد</div>
          )
        ) : (
          <>
            {order.map((cat) => {
              const list = items.filter((i) => i.cat === cat);
              if (!list.length) return null;
              const isOpen = open[cat] ?? false;
              return (
                <div key={cat} className="nc-group">
                  <button type="button" className="nc-group-head" onClick={() => setOpen({ ...open, [cat]: !isOpen })} style={{ ["--cat" as string]: categories[cat].color }}>
                    <span className="nc-dot" />
                    {categories[cat].title}
                    <span className="nc-count">{list.length}</span>
                    <ChevronDown size={14} className={`nc-chev${isOpen ? " open" : ""}`} />
                  </button>
                  {isOpen && list.map((it) => row(it))}
                </div>
              );
            })}
            <div className="nc-group">
              <button type="button" className="nc-group-head" onClick={() => setOpen({ ...open, api: !open.api })} style={{ ["--cat" as string]: categories.telegram.color }}>
                <Plug size={13} />
                همهٔ متدهای Bot API
                <span className="nc-count">{methods.length}</span>
                <ChevronDown size={14} className={`nc-chev${open.api ? " open" : ""}`} />
              </button>
              {open.api && (
                <>
                  <div className="nc-hint">نسخهٔ {botApi} · برای پیدا کردن سریع‌تر جست‌وجو کنید</div>
                  {methods.map((m) => row({ type: "tg." + m.name, label: m.name, summary: `${m.fields.filter((f) => f.required).length} پارامتر اجباری`, icon: "plug", cat: "telegram" }))}
                </>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
}
