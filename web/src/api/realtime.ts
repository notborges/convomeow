import { api } from "./client";
import type { PresenceEvent } from "./presence";
import type { Change } from "./realtime-cache";

export type RealtimeStatus = "connecting" | "connected" | "reconnecting";
const changeTypes = new Set([
  "accounts.changed",
  "conversations.changed",
  "attachment.changed",
  "contacts.changed",
  "avatars.changed",
]);

export function parseChange(data: string): Change | PresenceEvent | undefined {
  let value: unknown;
  try {
    value = JSON.parse(data);
  } catch {
    return;
  }
  if (!value || typeof value !== "object" || !("type" in value)) return;
  if (value.type === "presence.changed") {
    const event = value as Partial<PresenceEvent>;
    const p = event.presence;
    if (
      typeof event.account_id !== "string" ||
      typeof event.conversation_id !== "string" ||
      !p ||
      typeof p.participant_id !== "string" ||
      (p.display_name !== undefined && typeof p.display_name !== "string") ||
      !["typing", "recording", "paused"].includes(p.activity) ||
      typeof p.ttl_ms !== "number" ||
      !Number.isFinite(p.ttl_ms) ||
      p.ttl_ms < 0
    )
      return;
    return event as PresenceEvent;
  }
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
  receive: (change: Change | PresenceEvent) => void,
  status: (state: RealtimeStatus) => void,
  presenceAccountID?: string,
  activity?: () =>
    | { subscription_id: string; conversation_id?: string }
    | undefined,
) {
  let stopped = false;
  let socket: WebSocket | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let activityTimer: ReturnType<typeof setInterval> | undefined;
  let attempts = 0;
  const hidden = () => document.visibilityState === "hidden";
  function reportActivity() {
    const view = activity?.();
    const focused = !hidden() && document.hasFocus();
    if (view && socket?.readyState === WebSocket.OPEN)
      socket.send(
        JSON.stringify({
          type: "browser.activity",
          ...view,
          conversation_id: view.conversation_id ?? "",
          focused,
        }),
      );
    if (
      view?.conversation_id &&
      focused &&
      socket?.readyState === WebSocket.OPEN
    ) {
      activityTimer ??= setInterval(reportActivity, 20000);
    } else {
      clearInterval(activityTimer);
      activityTimer = undefined;
    }
  }
  function connect() {
    if (stopped || hidden()) return;
    const url = new URL("/api/v1/events", location.href);
    if (presenceAccountID)
      url.searchParams.set("presence_account_id", presenceAccountID);
    url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
    socket = new WebSocket(url);
    const current = socket;
    const handshake = setTimeout(() => current.close(), 10000);
    current.onmessage = (event) => {
      if (stopped || current !== socket || typeof event.data !== "string")
        return;
      const change = parseChange(event.data);
      if (!change) {
        current.close(4002, "Invalid event");
        return;
      }
      if (change.type === "ready") {
        clearTimeout(handshake);
        attempts = 0;
        status("connected");
        reportActivity();
      }
      receive(change);
    };
    current.onclose = async () => {
      clearTimeout(handshake);
      if (stopped || current !== socket) return;
      clearInterval(activityTimer);
      activityTimer = undefined;
      status("reconnecting");
      if (hidden()) return;
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
      if (stopped || current !== socket || hidden()) return;
      const delay = Math.min(30000, 1000 * 2 ** Math.min(attempts++, 5));
      timer = setTimeout(connect, delay * (0.75 + Math.random() * 0.5));
    };
  }
  function visibilityChanged() {
    clearTimeout(timer);
    if (hidden()) {
      reportActivity();
      status("reconnecting");
      socket?.close(1000, "Tab hidden");
    } else if (!socket || socket.readyState >= WebSocket.CLOSING) connect();
  }
  document.addEventListener("visibilitychange", visibilityChanged);
  window.addEventListener("focus", reportActivity);
  window.addEventListener("blur", reportActivity);
  window.addEventListener("convomeow:notification-view", reportActivity);
  status("connecting");
  connect();
  return () => {
    stopped = true;
    document.removeEventListener("visibilitychange", visibilityChanged);
    window.removeEventListener("focus", reportActivity);
    window.removeEventListener("blur", reportActivity);
    window.removeEventListener("convomeow:notification-view", reportActivity);
    clearInterval(activityTimer);
    clearTimeout(timer);
    socket?.close(1000, "Leaving app");
  };
}
