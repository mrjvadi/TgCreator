import { useContext, useMemo, useState } from "react";
import { CatalogContext } from "../context";
import { categories } from "../types";

export const DND_TYPE = "application/x-tgcreator-node";

const order = ["trigger", "telegram", "logic", "state", "redis", "db", "http"];

export default function Palette({ onAdd }: { onAdd: (type: string) => void }) {
  const { metas, methods, botApi } = useContext(CatalogContext);
  const [q, setQ] = useState("");
  const [showMethods, setShowMethods] = useState(false);
  const query = q.trim().toLowerCase();

  const groups = useMemo(() => {
    const byCat: Record<string, { type: string; label: string; icon?: string; desc: string }[]> = {};
    for (const nt of Object.values(metas)) {
      if (nt.name === "tg.") continue;
      const m = nt.meta;
      const hay = `${m.label} ${nt.name} ${nt.description}`.toLowerCase();
      if (query && !hay.includes(query)) continue;
      (byCat[m.category] ??= []).push({ type: nt.name, label: m.label, icon: m.icon, desc: nt.description });
    }
    return order.filter((c) => byCat[c]).map((c) => ({ cat: c, items: byCat[c] }));
  }, [metas, query]);

  const methodHits = useMemo(() => {
    const list = query ? methods.filter((m) => m.name.toLowerCase().includes(query)) : methods;
    return list.slice(0, query ? 60 : 200);
  }, [methods, query]);

  const drag = (type: string) => (e: React.DragEvent) => {
    e.dataTransfer.setData(DND_TYPE, type);
    e.dataTransfer.effectAllowed = "move";
  };

  return (
    <div className="palette">
      <input className="input" placeholder="جست‌وجوی نود یا متد…" value={q} onChange={(e) => setQ(e.target.value)} />
      <div className="palette-scroll">
        {groups.map((g) => (
          <div key={g.cat} className="pal-group">
            <div className="pal-title" style={{ ["--cat" as string]: categories[g.cat].color }}>
              {categories[g.cat].title}
            </div>
            {g.items.map((it) => (
              <button type="button" key={it.type} className="pal-item" draggable onDragStart={drag(it.type)} onClick={() => onAdd(it.type)} title={it.desc} style={{ ["--cat" as string]: categories[g.cat].color }}>
                <span className="pal-icon">{it.icon}</span>
                <span>{it.label}</span>
              </button>
            ))}
          </div>
        ))}
        <div className="pal-group">
          <button type="button" className="pal-title pal-toggle" style={{ ["--cat" as string]: categories.telegram.color }} onClick={() => setShowMethods(!showMethods)}>
            {showMethods || query ? "▾" : "▸"} همهٔ متدهای Bot API ({methods.length}) <small dir="ltr">{botApi}</small>
          </button>
          {(showMethods || query) &&
            methodHits.map((m) => (
              <button type="button" key={m.name} className="pal-item method" draggable onDragStart={drag("tg." + m.name)} onClick={() => onAdd("tg." + m.name)}>
                <span className="pal-icon">🔧</span>
                <code dir="ltr">{m.name}</code>
              </button>
            ))}
        </div>
      </div>
    </div>
  );
}
