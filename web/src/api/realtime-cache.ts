import type { Query, QueryClient } from "@tanstack/react-query";

export type Change = {
  type:
    | "ready"
    | "accounts.changed"
    | "conversations.changed"
    | "attachment.changed"
    | "contacts.changed"
    | "avatars.changed";
  account_id?: string;
  conversation_id?: string;
  attachment_id?: string;
};

export function affected(query: Query, change: Change): boolean {
  const [resource, id] = query.queryKey;
  if (change.type === "ready")
    return resource !== "session" && query.queryKey.at(-1) !== "download";
  if (change.type === "avatars.changed")
    return resource === "avatars" && id === change.account_id;
  if (change.type === "attachment.changed")
    return (
      resource === "attachment" &&
      id === change.attachment_id &&
      query.queryKey.length === 2
    );
  if (change.type === "accounts.changed")
    return (
      resource === "accounts" ||
      (resource === "login-attempt" && id === change.account_id) ||
      (resource === "contacts" && id === change.account_id)
    );
  if (
    change.type === "contacts.changed" &&
    (resource === "accounts" ||
      (resource === "contacts" && id === change.account_id))
  )
    return true;
  if (resource === "conversations") return id === change.account_id;
  if (resource === "messages" && change.type === "contacts.changed")
    return false;
  if (resource === "conversation" || resource === "messages")
    return change.conversation_id
      ? id === change.conversation_id
      : query.meta?.accountID === change.account_id;
  return false;
}

export function createRealtimeCache(client: QueryClient) {
  const dirty = new Map<string, Query>();
  const running = new Set<string>();
  let timer: ReturnType<typeof setTimeout> | undefined;
  let stopped = false;
  function schedule() {
    if (!stopped && !timer) timer = setTimeout(flush, 100);
  }
  async function refresh(query: Query) {
    running.add(query.queryHash);
    try {
      // Let an older request finish, then read a snapshot made after the notification.
      if (query.state.fetchStatus === "fetching")
        await query.promise?.catch(() => {});
      if (!stopped)
        await client.invalidateQueries(
          { queryKey: query.queryKey, exact: true, refetchType: "active" },
          { cancelRefetch: false },
        );
    } finally {
      running.delete(query.queryHash);
      if (dirty.has(query.queryHash)) schedule();
    }
  }
  function flush() {
    timer = undefined;
    for (const [hash, query] of dirty) {
      if (running.has(hash)) continue;
      dirty.delete(hash);
      void refresh(query);
    }
  }
  return {
    receive(change: Change) {
      if (stopped) return;
      for (const query of client
        .getQueryCache()
        .findAll({ predicate: (query) => affected(query, change) })) {
        if (change.type === "ready")
          void client.cancelQueries({ queryKey: query.queryKey, exact: true });
        dirty.set(query.queryHash, query);
      }
      schedule();
    },
    close() {
      stopped = true;
      clearTimeout(timer);
      dirty.clear();
    },
  };
}
