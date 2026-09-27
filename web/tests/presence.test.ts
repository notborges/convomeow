import { describe, expect, test } from "bun:test";
import { createPresenceStore, type PresenceEvent } from "../src/api/presence";
import { parseChange } from "../src/api/realtime";
import { createTypingSender } from "../src/features/presence/typing";

const event = (
  person: string,
  activity: "typing" | "recording" | "paused",
  ttl = 10000,
): PresenceEvent => ({
  type: "presence.changed",
  account_id: "one",
  conversation_id: "chat",
  presence: {
    participant_id: person,
    display_name: person,
    activity,
    ttl_ms: ttl,
  },
});

describe("transient presence", () => {
  test("updates participants independently, expires, and clears only the disconnected account", async () => {
    const store = createPresenceStore();
    store.receive(event("Maya", "typing", 30));
    store.receive(event("Oliver", "recording"));
    store.receive({
      ...event("Nora", "typing"),
      account_id: "two",
      conversation_id: "other",
    });
    expect(store.get("chat").map((p) => p.activity)).toEqual([
      "typing",
      "recording",
    ]);
    await Bun.sleep(50);
    expect(store.get("chat").map((p) => p.id)).toEqual(["Oliver"]);
    store.receive(event("Oliver", "paused"));
    expect(store.get("chat")).toHaveLength(0);
    store.receive(event("Maya", "typing"));
    store.clear("one");
    expect(store.get("chat")).toHaveLength(0);
    expect(store.get("other")).toHaveLength(1);
    store.clear();
    expect(store.get("other")).toHaveLength(0);
  });
  test("rejects malformed events and retains presence as a distinct event", () => {
    expect(parseChange(JSON.stringify(event("Maya", "typing")))).toEqual(
      event("Maya", "typing"),
    );
    for (const presence of [
      null,
      {},
      { participant_id: "Maya", activity: "online", ttl_ms: 1000 },
      { participant_id: "Maya", activity: "typing", ttl_ms: -1 },
    ]) {
      expect(
        parseChange(JSON.stringify({ ...event("Maya", "typing"), presence })),
      ).toBeUndefined();
    }
  });
  test("throttles keystrokes and sends pause after inactivity", async () => {
    const calls: string[] = [];
    const sender = createTypingSender(async (activity) => {
      calls.push(activity);
    });
    for (let i = 0; i < 50; i++) sender.input(true);
    expect(calls).toEqual(["typing"]);
    await Bun.sleep(4100);
    expect(calls).toEqual(["typing", "paused"]);
    sender.close();
    sender.input(true);
    expect(calls).toHaveLength(2);
  });
  test("serializes pause after in-flight typing and closes without reviving activity", async () => {
    const calls: string[] = [];
    let finish!: () => void;
    const pending = new Promise<void>((resolve) => {
      finish = resolve;
    });
    const sender = createTypingSender(async (activity) => {
      calls.push(activity);
      if (activity === "typing") await pending;
    });
    sender.input(true);
    sender.stop();
    sender.close();
    expect(calls).toEqual(["typing"]);
    finish();
    await Bun.sleep(0);
    expect(calls).toEqual(["typing", "paused"]);
  });
});
