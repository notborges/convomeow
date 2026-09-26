import { api } from "./client";
import type { Change } from "./realtime-cache";

export type RealtimeStatus = "connecting" | "connected" | "reconnecting";
const changeTypes = new Set([
  "accounts.changed",
  "conversations.changed",
  "attachment.changed",
  "contacts.changed",
  "avatars.changed",
]);

function parseChange(data: string): Change | undefined {
  let value: unknown;
  try {
    value = JSON.parse(data);
  } catch {
    return;
  }
  if (!value || typeof value !== "object" || !("type" in value)) return;
  if (value.type === "ready") return { type: "ready" };
  if (
    !changeTypes.has(String(value.type)) ||
    !("account_id" in value) ||
    typeof value.account_id !== "string"
  )
    return;
  if ("conversation_id" in value && typeof value.conversation_id !== "string")
    return;
  if (
    value.type === "attachment.changed" &&
    (!("attachment_id" in value) || typeof value.attachment_id !== "string")
  )
    return;
  return value as Change;
}

export function connectRealtime(
  receive: (change: Change) => void,
  status: (state: RealtimeStatus) => void,
) {
  let stopped = false;
  let socket: WebSocket | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let attempts = 0;
  function connect() {
    if (stopped) return;
    const url = new URL("/api/v1/events", location.href);
    url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
    socket = new WebSocket(url);
    const current = socket;
    const handshake = setTimeout(() => current.close(), 10000);
    current.onmessage = (event) => {
      if (stopped || typeof event.data !== "string") return;
      const change = parseChange(event.data);
      if (!change) {
        current.close(4002, "Invalid event");
        return;
      }
      if (change.type === "ready") {
        clearTimeout(handshake);
        attempts = 0;
        status("connected");
      }
      receive(change);
    };
    current.onclose = async () => {
      clearTimeout(handshake);
      if (stopped) return;
      status("reconnecting");
      try {
        const session = await api.session(AbortSignal.timeout(5000));
        if (stopped) return;
        if (!session.authenticated) {
          stopped = true;
          window.dispatchEvent(new Event("convomeow:unauthorized"));
          return;
        }
      } catch {
        /* Network loss is retried by the same reconnect loop. */
      }
      if (stopped) return;
      const delay = Math.min(30000, 1000 * 2 ** Math.min(attempts++, 5));
      timer = setTimeout(connect, delay * (0.75 + Math.random() * 0.5));
    };
  }
  status("connecting");
  connect();
  return () => {
    stopped = true;
    clearTimeout(timer);
    socket?.close(1000, "Leaving app");
  };
}
