import type { XUIInbound, XUIPanel } from "./types";
import type { BotMethod, Catalog, ChatSnapshot, SimEvent, TestSession, ValidateResult, Workflow } from "./types";

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  let data: unknown = undefined;
  try {
    data = text ? JSON.parse(text) : undefined;
  } catch {
    data = text;
  }
  if (!res.ok) {
    const msg = (data as { error?: string })?.error ?? `${res.status} ${res.statusText}`;
    throw new ApiError(msg, data);
  }
  return data as T;
}

export class ApiError extends Error {
  data: unknown;
  constructor(message: string, data: unknown) {
    super(message);
    this.data = data;
  }
}

export const api = {
  catalog: () => request<Catalog>("GET", "/api/catalog"),
  botMethods: () => request<{ version: string; methods: BotMethod[] }>("GET", "/api/botapi/methods"),
  examples: () => request<{ id: string; name: string; nodes: number }[]>("GET", "/api/examples"),
  example: (id: string) => request<Workflow>("GET", `/api/examples/${encodeURIComponent(id)}`),
  validate: (wf: Workflow) => request<ValidateResult>("POST", "/api/validate", wf),
  compose: async (wf: Workflow, assets: boolean) => {
    const res = await fetch(`/api/compose${assets ? "?assets=1" : ""}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(wf),
    });
    const text = await res.text();
    if (!res.ok) throw new Error(JSON.parse(text).error ?? text);
    return text;
  },
  xuiCheck: (p: XUIPanel) => request<{ ok: boolean; error?: string; inbounds?: XUIInbound[]; sub_url?: string }>("POST", "/api/xui/check", p),
  realtime: () => request<{ enabled: boolean; ws_url?: string; token?: string }>("GET", "/api/realtime"),
  testStart: (wf: Workflow) => request<TestSession>("POST", "/api/test", wf),
  testAction: (id: string, action: Record<string, unknown>) => request<{ ok: boolean; chat: ChatSnapshot }>("POST", `/api/test/${id}/action`, action),
  testChat: (id: string, chat: number) => request<ChatSnapshot>("GET", `/api/test/${id}/chats/${chat}`),
  testEvents: (id: string, after: number) => request<{ events: SimEvent[] | null }>("GET", `/api/test/${id}/events?after=${after}`),
  testStop: (id: string) => request<{ ok: boolean }>("DELETE", `/api/test/${id}`),
};

export function download(filename: string, content: string, type = "application/json") {
  const url = URL.createObjectURL(new Blob([content], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
