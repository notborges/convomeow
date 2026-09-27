import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../../api/client";
import { keys } from "../../api/queries";
import type { Message } from "../../api/types";

export function useMessageReaction(
  message: Message,
  onReact?: (emoji: string) => Promise<void>,
) {
  const client = useQueryClient();
  const refresh = () =>
    client.invalidateQueries({
      queryKey: keys.messages(message.conversation_id),
    });
  const mutation = useMutation({
    mutationKey: ["reaction", message.id],
    scope: { id: `reaction-${message.id}` },
    retry: false,
    mutationFn: async (emoji: string) => {
      if (onReact) {
        await onReact(emoji);
        return;
      }
      await api.setReaction(message.id, emoji);
    },
    onSettled: () => {
      void refresh();
    },
  });
  return {
    ...mutation,
    refresh: async () => {
      await refresh();
      mutation.reset();
    },
  };
}
