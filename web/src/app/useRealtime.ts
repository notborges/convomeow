import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import { presenceStore } from "../api/presence";
import { keys } from "../api/queries";
import { connectRealtime, type RealtimeStatus } from "../api/realtime";
import { createRealtimeCache } from "../api/realtime-cache";

export function useRealtime(
  enabled: boolean,
  accountID?: string,
  subscriptionID?: string,
  conversationID?: string,
) {
  const view = useRef({ subscriptionID, conversationID });
  view.current = { subscriptionID, conversationID };
  useEffect(() => {
    window.dispatchEvent(new Event("convomeow:notification-view"));
  }, [subscriptionID, conversationID]);
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
      () =>
        view.current.subscriptionID
          ? {
              subscription_id: view.current.subscriptionID,
              conversation_id: view.current.conversationID,
            }
          : undefined,
    );
    return () => {
      disconnect();
      cache.close();
      presenceStore.clear();
    };
  }, [client, enabled, presenceAccountID]);
  return status;
}
