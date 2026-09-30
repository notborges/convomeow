/// <reference lib="webworker" />
import { notificationContent, parseNotification } from "./content";

declare const self: ServiceWorkerGlobalScope;

self.addEventListener("install", (event) => {
  event.waitUntil(self.skipWaiting());
});
self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});
self.addEventListener("push", (event) => {
  let payload: ReturnType<typeof parseNotification>;
  try {
    payload = parseNotification(event.data?.json());
  } catch {
    /* Malformed pushes still require a visible notification. */
  }
  const { title, options } = notificationContent(payload);
  event.waitUntil(
    (async () => {
      let timer: ReturnType<typeof setTimeout> | undefined;
      const displayed = await Promise.race([
        self.registration
          .getNotifications({ tag: options.tag })
          .catch(() => []),
        new Promise<Notification[]>((resolve) => {
          timer = setTimeout(() => resolve([]), 250);
        }),
      ]).finally(() => clearTimeout(timer));
      const alert = {
        ...options,
        renotify: !displayed.some(
          (notification) =>
            notification.data?.messageID === payload?.message_id,
        ),
      };
      await self.registration.showNotification(title, alert);
    })(),
  );
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  event.waitUntil(
    (async () => {
      const path = event.notification.data?.path;
      const url = new URL(
        typeof path === "string" && path.startsWith("/app/") ? path : "/app/",
        self.location.origin,
      );
      const windows = await self.clients.matchAll({
        type: "window",
        includeUncontrolled: true,
      });
      const existing = windows.find(
        (client) =>
          new URL(client.url).origin === url.origin &&
          new URL(client.url).pathname.startsWith("/app/"),
      );
      if (existing?.url === url.href) {
        await existing.focus();
      } else if (existing) {
        const navigated = await existing.navigate(url.href);
        await (navigated ?? existing).focus();
      } else await self.clients.openWindow(url.href);
    })(),
  );
});
