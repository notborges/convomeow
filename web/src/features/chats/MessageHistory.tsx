import {
  Chat01Icon,
  Delete02Icon,
  Edit02Icon,
} from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { keys } from "../../api/queries";
import type { Message } from "../../api/types";
import { formatDate } from "../../i18n/format";
import { Button } from "../../ui/Button";
import { Dialog } from "../../ui/Dialog";
import { Loading } from "../../ui/Loading";

export function MessageHistory({
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
      "revisions",
      message.id,
    ],
    meta: { accountID: message.account_id },
    queryFn: ({ pageParam }) => api.messageRevisions(message.id, pageParam),
    initialPageParam: "",
    getNextPageParam: (page) => page.next_cursor,
  });
  return (
    <Dialog title={t(($) => $.messageChanges.history)} onClose={onClose}>
      {query.isPending ? (
        <Loading />
      ) : query.isError ? (
        <div role="alert">
          <p>{t(($) => $.errors.unavailable)}</p>
          <Button onClick={() => query.refetch()}>
            {t(($) => $.common.retry)}
          </Button>
        </div>
      ) : (
        <ol className="message-history">
          {query.data.pages
            .flatMap((page) => page.items)
            .map((revision) => (
              <li key={revision.id}>
                <span className="message-history__marker" aria-hidden="true">
                  <HugeiconsIcon
                    icon={
                      revision.kind === "original"
                        ? Chat01Icon
                        : revision.kind === "revoke"
                          ? Delete02Icon
                          : Edit02Icon
                    }
                    size={16}
                  />
                </span>
                <div className="message-history__entry">
                  <header>
                    <strong>
                      {t(($) =>
                        revision.kind === "original"
                          ? $.messageChanges.firstSaved
                          : revision.kind === "revoke"
                            ? $.messageChanges.deleted
                            : $.messageChanges.revisionEdited,
                      )}
                    </strong>
                    <time dateTime={revision.at}>
                      {formatDate(revision.at, {
                        dateStyle: "medium",
                        timeStyle: "medium",
                      })}
                    </time>
                  </header>
                  {revision.text && (
                    <p className="message-history__text">{revision.text}</p>
                  )}
                </div>
              </li>
            ))}
        </ol>
      )}
      <p className="message-history__note">
        {t(($) => $.messageChanges.historyDescription)}
      </p>
      {query.hasNextPage && (
        <Button
          busy={query.isFetchingNextPage}
          onClick={() => query.fetchNextPage()}
        >
          {t(($) => $.chats.loadingMore)}
        </Button>
      )}
    </Dialog>
  );
}
