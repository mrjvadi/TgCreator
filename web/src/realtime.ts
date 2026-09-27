import { Centrifuge, type PublicationContext } from "centrifuge";
import { api } from "./api";
import type { ChatSnapshot, SimEvent, TestSession } from "./types";

export interface LiveMessage {
  type: "event";
  event: SimEvent;
  chat?: ChatSnapshot;
}

export type LiveStatus = "connecting" | "live" | "polling" | "offline";

/**
 * Streams a test session's events. Uses Centrifugo over WebSocket when the
 * panel has it configured, otherwise polls the panel API.
 */
export async function follow(session: TestSession, onMessage: (m: LiveMessage) => void, onStatus: (s: LiveStatus) => void): Promise<() => void> {
  const info = await api.realtime().catch(() => ({ enabled: false }) as { enabled: boolean; ws_url?: string; token?: string });
  if (info.enabled && info.ws_url && session.sub_token) {
    onStatus("connecting");
    const client = new Centrifuge(info.ws_url, {
      token: info.token,
      getToken: async () => (await api.realtime()).token ?? "",
    });
    client.on("connecting", () => onStatus("connecting"));
    client.on("connected", () => onStatus("live"));
    client.on("disconnected", () => onStatus("offline"));
    const sub = client.newSubscription(session.channel, { token: session.sub_token });
    sub.on("publication", (ctx: PublicationContext) => onMessage(ctx.data as LiveMessage));
    sub.subscribe();
    client.connect();
    return () => {
      sub.unsubscribe();
      client.removeSubscription(sub);
      client.disconnect();
    };
  }

  onStatus("polling");
  let seq = 0;
  let stopped = false;
  const tick = async () => {
    if (stopped) return;
    try {
      const { events } = await api.testEvents(session.id, seq);
      const touched = new Set<number>();
      for (const e of events ?? []) {
        seq = Math.max(seq, e.seq);
        onMessage({ type: "event", event: e });
        if (e.chat_id) touched.add(e.chat_id);
      }
      for (const id of touched) onMessage({ type: "event", event: { seq: 0 } as SimEvent, chat: await api.testChat(session.id, id) });
    } catch {
      onStatus("offline");
    }
    if (!stopped) setTimeout(tick, 700);
  };
  tick();
  return () => {
    stopped = true;
  };
}
