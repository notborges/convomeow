import { expect, test } from "bun:test";

test("worker displays new messages, replaces retries quietly, and focuses the conversation", async () => {
  type WorkerEvent = {
    data?: { json: () => unknown };
    notification?: { close: () => void; data: { path: string } };
    waitUntil: (promise: Promise<unknown>) => void;
  };
  type Alert = NotificationOptions & { renotify: boolean };
  const listeners = new Map<string, (event: WorkerEvent) => void>();
  const shown: Alert[] = [];
  let displayed: { data: { messageID: string } }[] = [];
  let blockedLookup = false;
  let focused = 0;
  const navigated: string[] = [];
  const globals = globalThis as unknown as { self?: unknown };
  const previous = globals.self;
  globals.self = {
    addEventListener: (type: string, listener: (event: WorkerEvent) => void) =>
      listeners.set(type, listener),
    registration: {
      getNotifications: () =>
        blockedLookup ? new Promise(() => {}) : Promise.resolve(displayed),
      showNotification: async (_title: string, options: Alert) => {
        shown.push(options);
        displayed = [{ data: options.data }];
      },
    },
    location: { origin: "https://app.example" },
    clients: {
      matchAll: async () => [
        {
          url: "https://app.example/app/",
          navigate: async (url: string) => {
            navigated.push(url);
          },
          focus: async () => {
            focused++;
          },
        },
      ],
    },
  };
  try {
    await import("../src/features/notifications/worker");
    async function dispatch(
      type: string,
      event: Omit<WorkerEvent, "waitUntil">,
    ) {
      let work: Promise<unknown> | undefined;
      const listener = listeners.get(type);
      if (!listener) throw new Error("Missing worker listener");
      listener({
        ...event,
        waitUntil: (promise) => {
          work = promise;
        },
      });
      expect(work).toBeDefined();
      await work;
    }
    const payload = {
      version: 1,
      account_id: "account",
      conversation_id: "chat",
      message_id: "first",
      account_label: "Personal",
      conversation_name: "Maya",
      kind: "text",
      text: "Hello",
      locale: "en",
    };
    await dispatch("push", { data: { json: () => payload } });
    await dispatch("push", { data: { json: () => payload } });
    await dispatch("push", {
      data: { json: () => ({ ...payload, message_id: "next" }) },
    });
    expect(shown.map((options) => options.renotify)).toEqual([
      true,
      false,
      true,
    ]);
    expect(new Set(shown.map((options) => options.tag)).size).toBe(1);
    blockedLookup = true;
    await dispatch("push", {
      data: { json: () => ({ ...payload, message_id: "slow-lookup" }) },
    });
    expect(shown).toHaveLength(4);
    let closed = false;
    await dispatch("notificationclick", {
      notification: {
        data: shown[0].data,
        close: () => {
          closed = true;
        },
      },
    });
    expect(closed).toBe(true);
    expect(navigated).toEqual([
      "https://app.example/app/accounts/account/chats/chat",
    ]);
    expect(focused).toBe(1);
  } finally {
    globals.self = previous;
  }
});
