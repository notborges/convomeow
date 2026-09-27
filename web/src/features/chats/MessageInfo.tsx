import { useInfiniteQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { keys } from "../../api/queries";
import type { Message } from "../../api/types";
import { formatDate } from "../../i18n/format";
import { Button } from "../../ui/Button";
import { Dialog } from "../../ui/Dialog";
import { EmptyState } from "../../ui/EmptyState";
import { Loading } from "../../ui/Loading";

function receiptTime(value: string) {
  return formatDate(value, { dateStyle: "medium", timeStyle: "short" });
}

export function MessageInfo({
  message,
  onClose,
}: {
  message: Message;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const receipts = useInfiniteQuery({
    queryKey: [
      ...keys.messages(message.conversation_id),
      "receipts",
      message.id,
    ],
    meta: { accountID: message.account_id },
    queryFn: ({ pageParam }) => api.receipts(message.id, pageParam),
    initialPageParam: "",
    getNextPageParam: (page) => page.next_cursor,
  });
  const items = receipts.data?.pages.flatMap((page) => page.items) ?? [];
  return (
    <Dialog
      title={t(($) => $.receipts.info)}
      onClose={onClose}
      className="message-info"
    >
      <dl className="message-info__sent">
        <dt>{t(($) => $.receipts.sent)}</dt>
        <dd>{receiptTime(message.occurred_at)}</dd>
      </dl>
      {message.delivery?.group && (
        <p className="message-info__note">{t(($) => $.receipts.groupNote)}</p>
      )}
      {receipts.isPending ? (
        <Loading />
      ) : receipts.isError ? (
        <EmptyState title={t(($) => $.receipts.unavailable)}>
          <Button onClick={() => receipts.refetch()}>
            {t(($) => $.common.retry)}
          </Button>
        </EmptyState>
      ) : items.length === 0 ? (
        <p className="message-info__note">{t(($) => $.receipts.unknown)}</p>
      ) : (
        <ul className="receipt-list">
          {items.map((item) => (
            <li key={item.participant_id}>
              <strong>
                {item.display_name || item.participant_id.split("@")[0]}
              </strong>
              <dl>
                <dt>{t(($) => $.receipts.delivered)}</dt>
                <dd>
                  {item.delivered_at
                    ? receiptTime(item.delivered_at)
                    : item.read_at
                      ? t(($) => $.receipts.deliveredByRead)
                      : t(($) => $.receipts.notReported)}
                </dd>
                <dt>{t(($) => $.receipts.read)}</dt>
                <dd>
                  {item.read_at
                    ? receiptTime(item.read_at)
                    : t(($) => $.receipts.notReported)}
                </dd>
              </dl>
            </li>
          ))}
        </ul>
      )}
      {receipts.hasNextPage && (
        <Button
          busy={receipts.isFetchingNextPage}
          onClick={() => receipts.fetchNextPage()}
        >
          {t(($) => $.receipts.more)}
        </Button>
      )}
    </Dialog>
  );
}
