import { describe, expect, test } from "bun:test";
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { affected, createRealtimeCache } from "../src/api/realtime-cache";

const pause = () => new Promise((resolve) => setTimeout(resolve, 180));

describe("realtime cache", () => {
  test("coalesces bursts and leaves unrelated queries alone", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    let calls = 0;
    const observer = new QueryObserver(client, {
      queryKey: ["messages", "chat-a"],
      queryFn: () => ++calls,
      initialData: 0,
      staleTime: Infinity,
      meta: { accountID: "a" },
    });
    const unsubscribe = observer.subscribe(() => {});
    client.setQueryData(["messages", "chat-b"], 0);
    const cache = createRealtimeCache(client);
    for (let i = 0; i < 30; i++)
      cache.receive({
        type: "conversations.changed",
        account_id: "a",
        conversation_id: "chat-a",
      });
    await pause();
    expect(calls).toBe(1);
    expect(client.getQueryState(["messages", "chat-b"])?.isInvalidated).toBe(
      false,
    );
    cache.close();
    unsubscribe();
    client.clear();
  });

  test("a notification during a fetch requires a newer snapshot", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    let calls = 0;
    let finish!: (value: number) => void;
    const observer = new QueryObserver(client, {
      queryKey: ["attachment", "file"],
      initialData: 0,
      staleTime: Infinity,
      queryFn: () =>
        ++calls === 1
          ? new Promise<number>((resolve) => {
              finish = resolve;
            })
          : Promise.resolve(2),
    });
    const unsubscribe = observer.subscribe(() => {});
    const old = observer.refetch();
    const cache = createRealtimeCache(client);
    cache.receive({
      type: "attachment.changed",
      account_id: "a",
      attachment_id: "file",
    });
    await pause();
    finish(1);
    await old;
    await pause();
    expect(calls).toBe(2);
    expect(observer.getCurrentResult().data).toBe(2);
    cache.close();
    unsubscribe();
    client.clear();
  });

  test("reconnect marks inactive data stale without fetching it", async () => {
    const client = new QueryClient();
    client.setQueryData(["messages", "old-chat"], []);
    client.setQueryData(["attachment", "file", "download"], true);
    const cache = createRealtimeCache(client);
    cache.receive({ type: "ready" });
    await pause();
    expect(client.getQueryState(["messages", "old-chat"])?.isInvalidated).toBe(
      true,
    );
    expect(
      client.getQueryState(["attachment", "file", "download"])?.isInvalidated,
    ).toBe(false);
    cache.close();
    client.clear();
  });

  test("account-wide history refresh stays inside its account", () => {
    const client = new QueryClient();
    const query = client.getQueryCache().build(client, {
      queryKey: ["messages", "empty-chat"],
      meta: { accountID: "a" },
    });
    expect(
      affected(query, { type: "conversations.changed", account_id: "a" }),
    ).toBe(true);
    expect(
      affected(query, { type: "conversations.changed", account_id: "b" }),
    ).toBe(false);
    expect(affected(query, { type: "contacts.changed", account_id: "a" })).toBe(
      false,
    );
    client.clear();
  });
});
