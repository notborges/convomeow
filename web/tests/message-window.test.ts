import { describe, expect, test } from "bun:test";
import type { Message } from "../src/api/types";
import {
  chronologicalMessages,
  initialMessageIndex,
  messageGrouping,
  shiftedMessageIndex,
} from "../src/features/chats/messageWindow";

const message = (id: number): Message => ({
  id: String(id),
  account_id: "account",
  conversation_id: "chat",
  sender_id: "sender",
  direction: "inbound",
  state: "received",
  kind: "text",
  content: { text: String(id) },
  occurred_at: new Date(Date.UTC(2026, 0, 1, 12, id)).toISOString(),
});

describe("message window", () => {
  test("deduplicates page overlap without reordering same-time messages", () => {
    const a = message(1),
      b = { ...message(2), occurred_at: a.occurred_at };
    expect(
      chronologicalMessages([
        { items: [b, a] },
        { items: [a, message(0)] },
      ]).map((m) => m.id),
    ).toEqual(["0", "1", "2"]);
  });
  test("keeps virtual identities stable when either end is evicted", () => {
    const old = [3, 4, 5, 6].map(message);
    const older = [1, 2, 3, 4].map(message);
    const first = shiftedMessageIndex(old, older, initialMessageIndex);
    expect(first + older.findIndex((m) => m.id === "4")).toBe(
      initialMessageIndex + 1,
    );
    const newer = [3, 4, 5, 6].map(message);
    expect(shiftedMessageIndex(older, newer, first)).toBe(initialMessageIndex);
    expect(
      shiftedMessageIndex(old, [...old, message(7)], initialMessageIndex),
    ).toBe(initialMessageIndex);
  });
  test("group boundaries depend on the full window, not mounted rows", () => {
    expect(messageGrouping(message(3), message(2))).toEqual({
      showDay: false,
      grouped: true,
    });
    expect(
      messageGrouping({ ...message(3), sender_id: "other" }, message(2))
        .grouped,
    ).toBe(false);
    expect(
      messageGrouping(
        { ...message(3), occurred_at: "2026-01-02T00:00:00Z" },
        message(2),
      ),
    ).toEqual({ showDay: true, grouped: false });
  });
});
