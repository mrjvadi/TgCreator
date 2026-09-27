import {
  Activity, Bell, Bookmark, Braces, CircleStop, Database, DatabaseZap, Eraser, GitBranch, Globe, Image, Images, Keyboard,
  MessageSquare, MousePointerClick, Pencil, Plug, Repeat, ScrollText, Send, Server, ServerCog, ShieldCheck, Split,
  SquareTerminal, Timer, Trash2, UserCog, UserPlus, Users, UserSearch, UserX, Variable, Zap, type LucideIcon,
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
  "user-plus": UserPlus,
  "user-search": UserSearch,
  "user-cog": UserCog,
  "user-x": UserX,
  users: Users,
  server: Server,
  "server-cog": ServerCog,
  activity: Activity,
};

export function NodeIcon({ name, size = 18 }: { name?: string; size?: number }) {
  const Icon = (name && map[name]) || Plug;
  return <Icon size={size} strokeWidth={2} aria-hidden />;
}
