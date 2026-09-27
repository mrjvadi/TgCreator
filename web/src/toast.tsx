import { CircleAlert, CircleCheck, Info, X } from "lucide-react";
import { createContext, useCallback, useContext, useState } from "react";

type Kind = "info" | "success" | "error";
interface Toast {
  id: number;
  kind: Kind;
  text: string;
}

const Ctx = createContext<(text: string, kind?: Kind) => void>(() => {});
export const useToast = () => useContext(Ctx);

let seq = 0;

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [list, setList] = useState<Toast[]>([]);
  const push = useCallback((text: string, kind: Kind = "info") => {
    const id = ++seq;
    setList((l) => [...l.slice(-3), { id, kind, text }]);
    setTimeout(() => setList((l) => l.filter((t) => t.id !== id)), kind === "error" ? 7000 : 3200);
  }, []);
  const Icon = { info: Info, success: CircleCheck, error: CircleAlert };
  return (
    <Ctx.Provider value={push}>
      {children}
      <div className="toasts" role="status">
        {list.map((t) => {
          const I = Icon[t.kind];
          return (
            <div key={t.id} className={`toast toast-${t.kind}`}>
              <I size={16} />
              <span>{t.text}</span>
              <button type="button" className="toast-x" onClick={() => setList((l) => l.filter((x) => x.id !== t.id))} aria-label="بستن">
                <X size={14} />
              </button>
            </div>
          );
        })}
      </div>
    </Ctx.Provider>
  );
}
