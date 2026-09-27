import {
  Bell, Bookmark, Braces, CircleStop, Database, DatabaseZap, Eraser, GitBranch, Globe, Image, Images, Keyboard,
  MessageSquare, MousePointerClick, Pencil, Plug, Repeat, ScrollText, Send, ShieldCheck, Split, SquareTerminal,
  Timer, Trash2, Variable, Zap, type LucideIcon,
} from "lucide-react";

// Icon names come from the node catalog (lucide names).
const map: Record<string, LucideIcon> = {
  "square-terminal": SquareTerminal,
  "message-square": MessageSquare,
  "mouse-pointer-click": MousePointerClick,
  zap: Zap,
  send: Send,
  image: Image,
  images: Images,
  pencil: Pencil,
  keyboard: Keyboard,
  "trash-2": Trash2,
  bell: Bell,
  "shield-check": ShieldCheck,
  braces: Braces,
  plug: Plug,
  "git-branch": GitBranch,
  split: Split,
  variable: Variable,
  repeat: Repeat,
  timer: Timer,
  "circle-stop": CircleStop,
  "scroll-text": ScrollText,
  bookmark: Bookmark,
  eraser: Eraser,
  "database-zap": DatabaseZap,
  database: Database,
  globe: Globe,
};

export function NodeIcon({ name, size = 18 }: { name?: string; size?: number }) {
  const Icon = (name && map[name]) || Plug;
  return <Icon size={size} strokeWidth={2} aria-hidden />;
}
