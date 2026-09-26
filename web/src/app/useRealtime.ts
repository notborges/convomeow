import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { connectRealtime, type RealtimeStatus } from "../api/realtime";
import { createRealtimeCache } from "../api/realtime-cache";

export function useRealtime(enabled: boolean) {
  const client = useQueryClient();
  const [status, setStatus] = useState<RealtimeStatus>("connecting");
  useEffect(() => {
    if (!enabled) return;
    const cache = createRealtimeCache(client);
    const disconnect = connectRealtime(cache.receive, setStatus);
    return () => {
      disconnect();
      cache.close();
    };
  }, [client, enabled]);
  return status;
}
