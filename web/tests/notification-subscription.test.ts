import { expect, test } from "bun:test";
import { ApiError } from "../src/api/client";
import { NotificationSubscription } from "../src/features/notifications/subscription";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

async function until(condition: () => boolean) {
  for (let i = 0; i < 100 && !condition(); i++) await Bun.sleep(1);
  expect(condition()).toBe(true);
}

function browser() {
  const previous = new Map<string, PropertyDescriptor | undefined>();
  const storage = new Map<string, string>();
  const pageListeners = new Map<string, EventListener>();
  const page = {
    hasFocus: () => true,
    addEventListener: (type: string, listener: EventListener) =>
      pageListeners.set(type, listener),
    removeEventListener: (type: string) => pageListeners.delete(type),
  };
  const permission = { permission: "default" };
  const win = Object.assign(new EventTarget(), {
    isSecureContext: true,
    Notification: permission,
    PushManager: {},
  });
  const posts: { locale: string; preview: boolean }[] = [];
  const deletes: string[] = [];
  let current: PushSubscription | null = null;
  let calls = 0;
  let unsubscribed = 0;
  const subscription = {
    endpoint: "https://push.example/subscription",
    options: {},
    unsubscribe: async () => {
      current = null;
      unsubscribed++;
      return true;
    },
  } as unknown as PushSubscription;
  const controls = {
    create: async () => subscription,
    register: async () => ({ id: "browser-test" }),
  };
  const registration = {
    pushManager: {
      getSubscription: async () => current,
      subscribe: () => {
        calls++;
        return controls.create().then((value) => {
          current = value;
          permission.permission = "granted";
          return value;
        });
      },
    },
    getNotifications: async () => [],
  };
  const values = {
    document: page,
    window: win,
    Notification: permission,
    navigator: {
      serviceWorker: {
        register: async () => registration,
        ready: Promise.resolve(registration),
      },
    },
    localStorage: {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => {
        storage.set(key, value);
      },
    },
  };
  for (const [key, value] of Object.entries(values)) {
    previous.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { configurable: true, value });
  }
  const notifications = new NotificationSubscription({
    notificationConfig: async () => ({ enabled: true, public_key: "AQ" }),
    registerNotifications: async (_subscription, locale, preview) => {
      posts.push({ locale, preview });
      return controls.register();
    },
    deleteNotifications: async (id) => {
      deletes.push(id);
    },
  });
  const detach = notifications.listen();
  return {
    notifications,
    controls,
    posts,
    deletes,
    subscription,
    get calls() {
      return calls;
    },
    get unsubscribed() {
      return unsubscribed;
    },
    click() {
      pageListeners.get("click")?.({ type: "click", isTrusted: true } as Event);
    },
    previewFromAnotherTab(preview: boolean) {
      const key = "convomeow-notifications";
      storage.set(
        key,
        JSON.stringify({ enabled: true, prompted: true, preview }),
      );
      const event = new Event("storage");
      Object.defineProperty(event, "key", { value: key });
      win.dispatchEvent(event);
    },
    restore() {
      detach();
      for (const [key, descriptor] of previous) {
        if (descriptor) Object.defineProperty(globalThis, key, descriptor);
        else Reflect.deleteProperty(globalThis, key);
      }
    },
  };
}

test("subscriptions prompt once and reconcile locale, previews, and re-enabling", async () => {
  const fixture = browser();
  try {
    const { notifications, posts } = fixture;
    await notifications.updateSession(true, "en");
    expect(fixture.calls).toBe(0);
    fixture.click();
    fixture.click();
    await until(() => notifications.getSnapshot().status === "enabled");
    expect(fixture.calls).toBe(1);
    await notifications.updateSession(true, "pt-BR");
    expect(posts.at(-1)).toEqual({ locale: "pt-BR", preview: true });
    notifications.setPreview(false);
    await until(() => posts.at(-1)?.preview === false);
    fixture.previewFromAnotherTab(true);
    await until(() => posts.at(-1)?.preview === true);
    expect(notifications.getSnapshot().preview).toBe(true);
    await notifications.disable();
    expect(fixture.deletes).toEqual(["browser-test"]);
    expect(fixture.unsubscribed).toBe(1);
    notifications.enable();
    await until(() => notifications.getSnapshot().status === "enabled");
    expect(fixture.calls).toBe(2);
    await notifications.updateSession(false, "pt-BR");
    expect(notifications.getSnapshot().subscriptionID).toBeUndefined();
    expect(fixture.unsubscribed).toBe(2);
  } finally {
    fixture.restore();
  }
});

test("disabling cleans up a subscription created after permission settles", async () => {
  const fixture = browser();
  try {
    const creation = deferred<PushSubscription>();
    fixture.controls.create = () => creation.promise;
    await fixture.notifications.updateSession(true, "en");
    fixture.click();
    const disabled = fixture.notifications.disable();
    creation.resolve(fixture.subscription);
    await disabled;
    expect(fixture.posts).toHaveLength(0);
    expect(fixture.unsubscribed).toBe(1);
    expect(fixture.notifications.getSnapshot().status).toBe("disabled");
  } finally {
    fixture.restore();
  }
});

test("disabling revokes a registration that finishes after the request to disable", async () => {
  const fixture = browser();
  try {
    const registration = deferred<{ id: string }>();
    fixture.controls.register = () => registration.promise;
    await fixture.notifications.updateSession(true, "en");
    fixture.click();
    await until(() => fixture.posts.length === 1);
    const disabled = fixture.notifications.disable();
    registration.resolve({ id: "late-recipient" });
    await disabled;
    expect(fixture.deletes).toEqual(["late-recipient"]);
    expect(fixture.unsubscribed).toBe(1);
    expect(fixture.notifications.getSnapshot().subscriptionID).toBeUndefined();
  } finally {
    fixture.restore();
  }
});

test("browser push failures preserve the cause instead of reporting a dismissed prompt", async () => {
  const fixture = browser();
  try {
    fixture.controls.create = async () => {
      throw new DOMException(
        "Registration failed - push service unavailable",
        "AbortError",
      );
    };
    await fixture.notifications.updateSession(true, "en");
    fixture.click();
    await until(() => fixture.notifications.getSnapshot().status === "error");
    expect(fixture.notifications.getSnapshot().failure).toEqual({
      stage: "browser",
      detail: "AbortError: Registration failed - push service unavailable",
    });
    expect(fixture.posts).toHaveLength(0);
    fixture.controls.create = async () => fixture.subscription;
    fixture.notifications.enable();
    await until(() => fixture.notifications.getSnapshot().status === "enabled");
    expect(fixture.notifications.getSnapshot().failure).toBeUndefined();
  } finally {
    fixture.restore();
  }
});

test("server registration failures remain distinct from browser subscription failures", async () => {
  const fixture = browser();
  try {
    fixture.controls.register = async () => {
      throw new ApiError("Invalid subscription", 400);
    };
    await fixture.notifications.updateSession(true, "en");
    fixture.click();
    await until(() => fixture.notifications.getSnapshot().status === "error");
    expect(fixture.notifications.getSnapshot().failure?.stage).toBe("server");
    expect(fixture.notifications.getSnapshot().failure?.detail).toContain(
      "Invalid subscription",
    );
  } finally {
    fixture.restore();
  }
});
