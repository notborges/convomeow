import { useEffect, useRef } from "react";
import { api } from "../../api/client";
import { createTypingSender } from "./typing";

export function useTyping(conversationID: string, connected: boolean) {
  const sender = useRef<ReturnType<typeof createTypingSender> | undefined>(
    undefined,
  );
  useEffect(() => {
    if (!connected) return;
    const clientID = crypto.randomUUID();
    const current = createTypingSender((activity) =>
      api.presence(conversationID, activity, clientID),
    );
    sender.current = current;
    const stop = () => current.stop();
    const visibility = () => {
      if (document.visibilityState === "hidden") stop();
    };
    window.addEventListener("blur", stop);
    window.addEventListener("pagehide", stop);
    document.addEventListener("visibilitychange", visibility);
    return () => {
      current.close();
      sender.current = undefined;
      window.removeEventListener("blur", stop);
      window.removeEventListener("pagehide", stop);
      document.removeEventListener("visibilitychange", visibility);
    };
  }, [conversationID, connected]);
  return {
    input: (hasText: boolean) => {
      if (document.visibilityState !== "hidden") sender.current?.input(hasText);
    },
    stop: () => sender.current?.stop(),
  };
}
