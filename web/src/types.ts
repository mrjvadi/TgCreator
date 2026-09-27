// Mirrors internal/workflow (the file the runtime executes) and the
// panel API payloads.

export type Json = null | boolean | number | string | Json[] | { [k: string]: Json };

export interface WorkflowNode {
  id: string;
  type: string;
  name?: string;
  params?: Record<string, Json>;
  disabled?: boolean;
  continue_on_error?: boolean;
  position?: [number, number];
}

export interface VPNPanel {
  type?: string;
  url: string;
  username?: string;
  password?: string;
  token?: string;
  totp_secret?: string;
  sub_url?: string;
  address?: string;
  api_path?: string;
  insecure_tls?: boolean;
  timeout?: string;
}

export interface VPNGroup {
  id: string;
  name: string;
  protocol?: string;
  port?: number;
  users: number;
  enabled: boolean;
}

export interface Database {
  driver: string;
  dsn: string;
  max_open_conns?: number;
  max_idle_conns?: number;
  migrations?: string[];
  image?: string;
}

export interface Workflow {
  name: string;
  version?: number;
  bot: {
    token: string;
    api_url?: string;
    mode?: string;
    parse_mode?: string;
    webhook?: { url?: string; listen?: string; path?: string; secret_token?: string; max_connections?: number };
  };
  runtime?: {
    workers?: number;
    queue_size?: number;
    max_steps?: number;
    drop_pending?: boolean;
    log_level?: string;
    state_backend?: string;
    state_ttl?: string;
    health_listen?: string;
    handle_timeout?: string;
  };
  services?: { redis?: { url: string; image?: string }; databases?: Record<string, Database>; vpn?: Record<string, VPNPanel> };
  variables?: Record<string, Json>;
  nodes: WorkflowNode[];
  connections: Record<string, Record<string, string[]>>;
}

export interface Param {
  name: string;
  label: string;
  type: string;
  required?: boolean;
  default?: Json;
  options?: string[];
  placeholder?: string;
  help?: string;
}

export interface NodeMeta {
  label: string;
  summary: string;
  category: string;
  icon?: string;
  outputs: string[];
  case_outputs?: string;
  params: Param[];
  method?: string;
}

export interface NodeType {
  name: string;
  description: string;
  meta: NodeMeta;
}

export interface Catalog {
  nodes: NodeType[];
  bot_api: string;
  update_types: string[];
}

export interface BotField {
  name: string;
  types: string[];
  required: boolean;
}

export interface BotMethod {
  name: string;
  fields: BotField[];
}

export interface Issue {
  level: "error" | "warning";
  node?: string;
  message: string;
}

export interface ValidateResult {
  ok: boolean;
  issues: Issue[] | null;
  requirements?: string[];
  updates?: string[];
}

// ---- test chat ----

export interface ButtonView {
  text: string;
  data?: string;
  url?: string;
}

export interface MsgView {
  id: number;
  chat_id: number;
  from: string;
  from_id?: number;
  by_bot: boolean;
  text?: string;
  caption?: string;
  media?: string;
  info?: string;
  buttons?: ButtonView[][];
  deleted?: boolean;
  pinned?: boolean;
  reactions?: string[];
  reply_to?: number;
}

export interface ChatView {
  id: number;
  type: string;
  title: string;
}

export interface ChatSnapshot {
  chat: ChatView;
  messages: MsgView[];
  keyboard?: { text: string; request_contact?: boolean }[][];
  members?: { user_id: number; name: string; status: string }[];
}

export interface SimEvent {
  seq: number;
  at: string;
  chat_id: number;
  chat: string;
  who: string;
  text: string;
  kind: string;
}

export interface TestSession {
  id: string;
  channel: string;
  sub_token?: string;
  users: { id: number; name: string }[];
  chats: ChatView[];
}

export const categories: Record<string, { title: string; color: string }> = {
  trigger: { title: "شروع‌کننده‌ها", color: "var(--c-trigger)" },
  telegram: { title: "تلگرام", color: "var(--c-telegram)" },
  logic: { title: "منطق و جریان", color: "var(--c-logic)" },
  state: { title: "وضعیت کاربر", color: "var(--c-state)" },
  redis: { title: "Redis", color: "var(--c-redis)" },
  db: { title: "دیتابیس", color: "var(--c-db)" },
  http: { title: "HTTP", color: "var(--c-http)" },
  vpn: { title: "پنل VPN", color: "var(--c-vpn)" },
  other: { title: "سایر", color: "var(--muted)" },
};
