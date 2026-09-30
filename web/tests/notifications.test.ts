import { describe, expect, test } from "bun:test";
import {
  notificationContent,
  parseNotification,
} from "../src/features/notifications/content";
import { armNotificationPrompt } from "../src/features/notifications/permission";

describe("browser notifications", () => {
  test("prompts once inside a trusted interaction and respects ineligible state", () => {
    const listeners = new Map<string, EventListener>();
    const target = {
      addEventListener: (type: string, listener: EventListener) =>
        listeners.set(type, listener),
      removeEventListener: (type: string) => listeners.delete(type),
    } as unknown as EventTarget;
    let allowed = false;
    let calls = 0;
    const close = armNotificationPrompt(
      target,
      () => allowed,
      () => calls++,
    );
    const interaction = (trusted: boolean) =>
      listeners.get("click")?.({ type: "click", isTrusted: trusted } as Event);
    interaction(true);
    expect(calls).toBe(0);
    allowed = true;
    interaction(false);
    expect(calls).toBe(0);
    interaction(true);
    expect(calls).toBe(1);
    interaction(true);
    expect(calls).toBe(1);
    close();
  });

  test("renders localized text and media alerts with safe destinations and stable tags", () => {
    const payload = parseNotification({
      version: 1,
      account_id: "personal",
      conversation_id: "chat/with?characters",
      message_id: "message",
      account_label: "Personal",
      conversation_name: "Maya",
      kind: "image",
      locale: "pt-BR",
    });
    expect(payload).toBeDefined();
    if (!payload) throw new Error("Expected a valid notification payload");
    const content = notificationContent(payload);
    expect(content.title).toBe("Maya · Personal");
    expect(content.options.body).toBe("Foto");
    expect(content.options.data.path).toBe(
      "/app/accounts/personal/chats/chat%2Fwith%3Fcharacters",
    );
    expect(
      notificationContent({ ...payload, message_id: "another" }).options.tag,
    ).toBe(content.options.tag);
    expect(
      notificationContent({ ...payload, kind: "", text: undefined }).options
        .body,
    ).toBe("Nova mensagem");
    expect(
      notificationContent({ ...payload, text: "<b>User content</b>" }).options
        .body,
    ).toBe("<b>User content</b>");
    for (const value of [
      null,
      {},
      { version: 2 },
      { ...payload, locale: "unsupported" },
      { ...payload, text: "x".repeat(1001) },
    ])
      expect(parseNotification(value)).toBeUndefined();
  });
});
