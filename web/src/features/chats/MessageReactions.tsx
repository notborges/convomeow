import { useInfiniteQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { keys } from "../../api/queries";
import type { Message } from "../../api/types";
import { Avatar } from "../../ui/Avatar";
import { Button } from "../../ui/Button";
import { Dialog } from "../../ui/Dialog";
import { Loading } from "../../ui/Loading";

export function MessageReactions({ message }: { message: Message }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const reactions = message.reactions ?? [];
  const count = reactions.reduce(
    (total, reaction) => total + reaction.count,
    0,
  );
  const own = reactions.some((reaction) => reaction.own);
  const emoji = reactions
    .slice(0, 3)
    .map((reaction) => reaction.emoji)
    .join(" ");
  return (
    <>
      {count > 0 && (
        <button
          type="button"
          className={`message-reactions ${count === 1 ? "message-reactions--single" : ""}`}
          aria-label={t(
            ($) => (own ? $.reactions.ownSummary : $.reactions.summary),
            { emoji, count },
          )}
          onClick={() => setOpen(true)}
        >
          <span className="message-reactions__emoji" aria-hidden="true">
            {reactions.slice(0, 3).map((reaction) => (
              <span key={reaction.emoji}>{reaction.emoji}</span>
            ))}
          </span>
          {count > 1 && (
            <span className="message-reactions__count" aria-hidden="true">
              {count}
            </span>
          )}
        </button>
      )}
      {open && (
        <ReactionDetails message={message} onClose={() => setOpen(false)} />
      )}
    </>
  );
}

function ReactionDetails({
  message,
  onClose,
}: {
  message: Message;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const query = useInfiniteQuery({
    queryKey: [
      ...keys.messages(message.conversation_id),
      "reactions",
      message.id,
    ],
    meta: { accountID: message.account_id },
    queryFn: ({ pageParam }) => api.reactions(message.id, pageParam),
    initialPageParam: "",
    getNextPageParam: (page) => page.next_cursor,
  });
  const items = query.data?.pages.flatMap((page) => page.items) ?? [];
  return (
    <Dialog
      title={t(($) => $.reactions.title)}
      onClose={onClose}
      className="reaction-details"
    >
      {query.isPending ? (
        <Loading />
      ) : query.isError ? (
        <div role="alert">
          <p>{t(($) => $.reactions.unavailable)}</p>
          <Button onClick={() => query.refetch()}>
            {t(($) => $.common.retry)}
          </Button>
        </div>
      ) : items.length === 0 ? (
        <p>{t(($) => $.reactions.empty)}</p>
      ) : (
        <ul className="reaction-details__list">
          {items.map((item) => {
            const name = item.is_own
              ? t(($) => $.reactions.you)
              : item.display_name || item.participant_id.split("@")[0];
            return (
              <li key={item.participant_id}>
                <Avatar name={name} url={item.avatar_url} size="small" />
                <span className="reaction-details__name">{name}</span>
                <span className="reaction-details__emoji">{item.emoji}</span>
              </li>
            );
          })}
        </ul>
      )}
      {query.hasNextPage && (
        <Button
          busy={query.isFetchingNextPage}
          onClick={() => query.fetchNextPage()}
        >
          {t(($) => $.reactions.more)}
        </Button>
      )}
    </Dialog>
  );
}
