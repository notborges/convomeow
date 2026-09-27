import { useInfiniteQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { api } from "../../api/client";
import { keys } from "../../api/queries";
import { chronologicalMessages } from "./messageWindow";

type Position = { around?: string; cursor?: string; after?: string };
export const messageWindowKey = (id: string, around: string) => [
  ...keys.messages(id),
  { around },
];

export function useMessageWindow(
  accountID: string,
  conversationID: string,
  around: string,
) {
  const query = useInfiniteQuery({
    queryKey: messageWindowKey(conversationID, around),
    meta: { accountID },
    queryFn: ({ pageParam, signal }) =>
      api.messages(conversationID, pageParam.cursor, pageParam, signal),
    initialPageParam: { around } as Position,
    getNextPageParam: (page): Position | undefined =>
      page.next_cursor ? { cursor: page.next_cursor } : undefined,
    getPreviousPageParam: (page): Position | undefined =>
      page.previous_cursor ? { after: page.previous_cursor } : undefined,
    maxPages: 10,
    gcTime: 60_000,
    retry: 1,
  });
  const items = useMemo(
    () => chronologicalMessages(query.data?.pages ?? []),
    [query.data],
  );
  return { ...query, items };
}
