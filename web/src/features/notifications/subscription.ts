import { ApiError, api } from "../../api/client";
import { applicationServerKey, armNotificationPrompt } from "./permission";

type NotificationStatus =
  | "loading"
  | "unsupported"
  | "unavailable"
  | "blocked"
  | "ready"
  | "dismissed"
  | "subscribing"
  | "enabled"
  | "disabled"
  | "error";
type Preferences = { enabled: boolean; preview: boolean; prompted: boolean };
type FailureStage = "setup" | "browser" | "server";
type State = {
  status: NotificationStatus;
  subscriptionID?: string;
  preview: boolean;
  failure?: { stage: FailureStage; detail?: string };
};
type NotificationAPI = Pick<
  typeof api,
  "notificationConfig" | "registerNotifications" | "deleteNotifications"
>;
const preferenceKey = "convomeow-notifications";
const defaults: Preferences = { enabled: true, preview: true, prompted: false };

function readPreferences(fallback = defaults): Preferences {
  try {
    const value = JSON.parse(localStorage.getItem(preferenceKey) ?? "{}");
    return {
      enabled: value.enabled !== false,
      preview: value.preview !== false,
      prompted: value.prompted === true,
    };
  } catch {
    return fallback;
  }
}

function withSubscriptionLock<T>(task: () => Promise<T>): Promise<T> {
  return navigator.locks
    ? navigator.locks.request(preferenceKey, task)
    : task();
}

export class NotificationSubscription {
  private prefs = readPreferences();
  private state: State = { status: "loading", preview: this.prefs.preview };
  private listeners = new Set<() => void>();
  private active = false;
  private locale = "en";
  private revision = 0;
  private abort?: AbortController;
  private worker?: {
    registration: ServiceWorkerRegistration;
    key: ArrayBuffer;
  };
  private cleanupPrompt = () => {};
  private pending = Promise.resolve();
  private subscribing = false;
  private registeredID?: string;

  constructor(private readonly service: NotificationAPI = api) {}

  getSnapshot = () => this.state;
  subscribeToState = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  listen() {
    window.addEventListener("storage", this.preferencesChanged);
    window.addEventListener("focus", this.focused);
    return () => {
      window.removeEventListener("storage", this.preferencesChanged);
      window.removeEventListener("focus", this.focused);
      this.active = false;
      this.invalidate();
    };
  }

  async updateSession(authenticated: boolean, locale: string) {
    const changed = this.active !== authenticated;
    const languageChanged = this.locale !== locale;
    this.active = authenticated;
    this.locale = locale;
    if (changed) {
      if (authenticated) await this.setup();
      else await this.clear().catch(() => {});
    } else if (authenticated && languageChanged && this.registeredID) {
      await this.refresh();
    }
  }

  enable = () => {
    if (!("Notification" in window) || Notification.permission === "denied")
      return;
    this.savePreferences({ enabled: true, prompted: false });
    if (this.worker) this.subscribePush();
    else void this.setup();
  };

  disable = async () => {
    this.savePreferences({ enabled: false });
    try {
      await this.clear(true);
    } catch (error) {
      this.savePreferences({ enabled: true });
      this.update({
        status: this.registeredID ? "enabled" : "error",
        subscriptionID: this.registeredID,
      });
      throw error;
    }
  };

  setPreview = (preview: boolean) => {
    this.savePreferences({ preview });
    if (this.active && this.registeredID) void this.refresh();
  };

  private update(next: Partial<State>) {
    this.state = {
      ...this.state,
      failure: next.status ? undefined : this.state.failure,
      ...next,
    };
    for (const listener of this.listeners) listener();
  }

  private fail(error: unknown, stage: FailureStage, revision: number) {
    if (!this.current(revision)) return;
    const detail =
      error instanceof Error ? `${error.name}: ${error.message}` : undefined;
    this.update({ status: "error", failure: { stage, detail } });
  }

  private savePreferences(next: Partial<Preferences>) {
    this.prefs = { ...this.prefs, ...next };
    this.update({ preview: this.prefs.preview });
    try {
      localStorage.setItem(preferenceKey, JSON.stringify(this.prefs));
    } catch {
      /* Preferences remain usable when browser storage is unavailable. */
    }
  }

  private invalidate() {
    this.abort?.abort();
    this.cleanupPrompt();
    return ++this.revision;
  }

  private current(revision: number) {
    return this.active && revision === this.revision;
  }

  private enqueue(task: () => Promise<void>) {
    this.pending = this.pending.catch(() => {}).then(task);
    return this.pending;
  }

  private async persist(subscription: PushSubscription, revision: number) {
    const prefs = readPreferences(this.prefs);
    if (!this.current(revision) || !prefs.enabled) return;
    try {
      const result = await this.service.registerNotifications(
        subscription,
        this.locale,
        prefs.preview,
      );
      // Cleanup needs the server ID even if logout occurred during registration.
      this.registeredID = result.id;
      if (this.current(revision))
        this.update({ status: "enabled", subscriptionID: result.id });
    } catch (error) {
      this.fail(error, "server", revision);
    }
  }

  private refresh(revision = this.revision) {
    const worker = this.worker;
    if (!worker) return Promise.resolve();
    return this.enqueue(() =>
      withSubscriptionLock(async () => {
        if (!this.current(revision) || !this.prefs.enabled) return;
        let subscription =
          await worker.registration.pushManager.getSubscription();
        const oldKey = subscription?.options.applicationServerKey;
        const key = new Uint8Array(worker.key);
        if (
          subscription &&
          oldKey &&
          (oldKey.byteLength !== key.byteLength ||
            !new Uint8Array(oldKey).every((byte, index) => byte === key[index]))
        ) {
          await subscription.unsubscribe();
          subscription = null;
        }
        if (!this.current(revision)) return;
        if (subscription && Notification.permission === "granted")
          await this.persist(subscription, revision);
        else {
          this.update({
            status: this.prefs.prompted ? "dismissed" : "ready",
            subscriptionID: undefined,
          });
          this.arm();
        }
      }),
    ).catch((error) => {
      this.fail(error, "browser", revision);
    });
  }

  private async setup() {
    const revision = this.invalidate();
    this.worker = undefined;
    this.update({ status: "loading", subscriptionID: undefined });
    if (
      !window.isSecureContext ||
      !("serviceWorker" in navigator) ||
      !("PushManager" in window) ||
      !("Notification" in window)
    ) {
      this.update({ status: "unsupported" });
      return;
    }
    const abort = new AbortController();
    this.abort = abort;
    try {
      const config = await this.service.notificationConfig(abort.signal);
      if (!this.current(revision)) return;
      if (!config.enabled || !config.public_key) {
        this.update({ status: "unavailable" });
        return;
      }
      const registration = await navigator.serviceWorker.register(
        "/app/notifications-sw.js",
        { scope: "/app/", updateViaCache: "none" },
      );
      let timer: ReturnType<typeof setTimeout> | undefined;
      await Promise.race([
        navigator.serviceWorker.ready,
        new Promise<never>((_resolve, reject) => {
          timer = setTimeout(
            () => reject(new Error("Worker activation timed out")),
            10000,
          );
        }),
      ]).finally(() => clearTimeout(timer));
      if (!this.current(revision)) return;
      this.worker = {
        registration,
        key: applicationServerKey(config.public_key),
      };
      if (!this.prefs.enabled) this.update({ status: "disabled" });
      else if (Notification.permission === "denied")
        this.update({ status: "blocked" });
      else await this.refresh(revision);
    } catch (error) {
      this.fail(error, "setup", revision);
    }
  }

  private arm() {
    this.cleanupPrompt();
    this.cleanupPrompt = armNotificationPrompt(
      document,
      () =>
        this.active &&
        document.hasFocus() &&
        this.prefs.enabled &&
        Notification.permission !== "denied" &&
        (Notification.permission === "granted" ||
          !readPreferences(this.prefs).prompted),
      this.subscribePush,
    );
  }

  private subscribePush = () => {
    const worker = this.worker;
    if (
      !worker ||
      !this.active ||
      this.subscribing ||
      Notification.permission === "denied"
    )
      return;
    const revision = this.invalidate();
    this.subscribing = true;
    this.savePreferences({ enabled: true, prompted: true });
    this.update({ status: "subscribing" });
    // Start in the trusted gesture before awaiting locks or other work.
    let request: Promise<PushSubscription>;
    try {
      request = worker.registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: worker.key,
      });
    } catch (error) {
      this.subscribing = false;
      this.fail(error, "browser", revision);
      return;
    }
    void this.enqueue(async () => {
      const subscription = await request;
      await withSubscriptionLock(() => this.persist(subscription, revision));
    })
      .catch((error) => {
        if (!this.current(revision)) return;
        if (Notification.permission === "denied")
          this.update({ status: "blocked" });
        else if (
          Notification.permission === "default" &&
          error instanceof DOMException &&
          error.name === "NotAllowedError"
        )
          this.update({ status: "dismissed" });
        else this.fail(error, "browser", revision);
      })
      .finally(() => {
        this.subscribing = false;
      });
  };

  private async clear(revoke = false) {
    const revision = this.invalidate();
    const registration = this.worker?.registration;
    this.update({ status: "disabled", subscriptionID: undefined });
    await this.enqueue(() =>
      withSubscriptionLock(async () => {
        if (revision !== this.revision) return;
        if (revoke && this.registeredID) {
          try {
            await this.service.deleteNotifications(this.registeredID);
          } catch (error) {
            if (!(error instanceof ApiError && error.status === 404))
              throw error;
          }
        }
        this.registeredID = undefined;
        const subscription = await registration?.pushManager.getSubscription();
        await subscription?.unsubscribe();
      }),
    );
    void registration
      ?.getNotifications()
      .then((notifications) => {
        if (revision === this.revision)
          for (const notification of notifications) notification.close();
      })
      .catch(() => {});
  }

  private preferencesChanged = (event: StorageEvent) => {
    if (event.key !== preferenceKey) return;
    const previous = this.prefs;
    this.prefs = readPreferences(this.prefs);
    this.update({ preview: this.prefs.preview });
    if (!this.active) return;
    if (!this.prefs.enabled)
      void this.clear(true).catch(() => this.update({ status: "error" }));
    else if (!previous.enabled) void this.setup();
    else if (previous.preview !== this.prefs.preview && this.registeredID)
      void this.refresh();
  };

  private focused = () => {
    if (
      this.active &&
      this.worker &&
      this.prefs.enabled &&
      !this.registeredID &&
      !this.subscribing &&
      Notification.permission === "granted"
    )
      void this.refresh();
  };
}
