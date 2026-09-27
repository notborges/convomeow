import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { presenceStore } from "../api/presence";
import { connectRealtime, type RealtimeStatus } from "../api/realtime";
import { createRealtimeCache } from "../api/realtime-cache";

export function useRealtime(enabled: boolean, accountID?: string) {
  const client = useQueryClient();
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
      accountID,
    );
    return () => {
      disconnect();
      cache.close();
      presenceStore.clear();
    };
  }, [client, enabled, accountID]);
  return status;
}
