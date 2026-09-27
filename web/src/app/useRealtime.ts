import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api } from "../api/client";
import { presenceStore } from "../api/presence";
import { keys } from "../api/queries";
import { connectRealtime, type RealtimeStatus } from "../api/realtime";
import { createRealtimeCache } from "../api/realtime-cache";

export function useRealtime(enabled: boolean, accountID?: string) {
  const client = useQueryClient();
  const accounts = useQuery({
    queryKey: keys.accounts,
    queryFn: api.accounts,
    enabled,
  });
  const presenceAccountID = accounts.data?.items.find(
    (account) =>
      account.id === accountID && account.capabilities?.includes("typing"),
  )?.id;
  const [status, setStatus] = useState<RealtimeStatus>("connecting");
  useEffect(() => {
    if (!enabled) return;
    const cache = createRealtimeCache(client);
    const disconnect = connectRealtime(
      (event) => {
        if (event.type === "presence.changed") {
          presenceStore.receive(event);
          return;
        }
        if (event.type === "ready") presenceStore.clear();
        if (event.type === "accounts.changed")
          presenceStore.clear(event.account_id);
        cache.receive(event);
      },
      (state) => {
        setStatus(state);
        if (state !== "connected") presenceStore.clear();
      },
      presenceAccountID,
    );
    return () => {
      disconnect();
      cache.close();
      presenceStore.clear();
    };
  }, [client, enabled, presenceAccountID]);
  return status;
}
