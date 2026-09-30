import { createInstance } from "i18next";
import { notifications as en } from "../../i18n/locales/en.json";
import { notifications as ptBR } from "../../i18n/locales/pt-BR.json";

const translations = createInstance();
void translations.init({
  resources: {
    en: { translation: { notifications: en } },
    "pt-BR": { translation: { notifications: ptBR } },
  },
  fallbackLng: "en",
  initAsync: false,
  interpolation: { escapeValue: false },
});

export type NotificationPayload = {
  version: 1;
  account_id: string;
  conversation_id: string;
  message_id: string;
  account_label: string;
  conversation_name: string;
  kind: string;
  text?: string;
  locale: "en" | "pt-BR";
};

export function parseNotification(
  value: unknown,
): NotificationPayload | undefined {
  if (!value || typeof value !== "object") return;
  const p = value as Partial<NotificationPayload>;
  if (
    p.version !== 1 ||
    !p.account_id ||
    !p.conversation_id ||
    !p.message_id ||
    ![
      p.account_id,
      p.conversation_id,
      p.message_id,
      p.account_label,
      p.conversation_name,
      p.kind,
    ].every((item) => typeof item === "string" && item.length <= 256) ||
    (p.text !== undefined &&
      (typeof p.text !== "string" || p.text.length > 1000)) ||
    !["en", "pt-BR"].includes(p.locale ?? "")
  )
    return;
  return p as NotificationPayload;
}

export function notificationContent(payload?: NotificationPayload) {
  const t = translations.getFixedT(payload?.locale ?? "en");
  const media = [
    "image",
    "video",
    "audio",
    "document",
    "sticker",
    "location",
    "contact",
  ] as const;
  const kind = media.find((kind) => kind === payload?.kind);
  const path = payload
    ? `/app/accounts/${encodeURIComponent(payload.account_id)}/chats/${encodeURIComponent(payload.conversation_id)}`
    : "/app/";
  return {
    title: payload
      ? `${payload.conversation_name || t(($) => $.notifications.sender)} · ${payload.account_label}`
      : "ConvoMeow",
    options: {
      body:
        payload?.text ||
        (kind
          ? t(($) => $.notifications.media[kind])
          : t(($) => $.notifications.newMessage)),
      icon: "/app/brand/convomeow.png",
      badge: "/app/brand/convomeow.png",
      tag: payload
        ? `convomeow:${payload.account_id}:${payload.conversation_id}`
        : "convomeow",
      data: { path, messageID: payload?.message_id },
    } satisfies NotificationOptions,
  };
}
