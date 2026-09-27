import { Tick01Icon, TickDouble01Icon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useTranslation } from "react-i18next";
import type { Message } from "../../api/types";
import { stateLabel } from "../../i18n/format";

export function DeliveryStatus({ message }: { message: Message }) {
  const { t } = useTranslation();
  const delivery = message.delivery;
  const known = delivery && delivery.state !== "unknown";
  if (!known && message.state !== "sent")
    return <span>{stateLabel(message.state)}</span>;
  const read = delivery?.state === "read";
  const complete = read || delivery?.state === "delivered";
  const label =
    delivery?.group && known
      ? delivery.read_count > 0
        ? t(($) => $.receipts.readCount, { count: delivery.read_count })
        : t(($) => $.receipts.deliveredCount, {
            count: delivery.delivered_count,
          })
      : read
        ? t(($) => $.receipts.read)
        : complete
          ? t(($) => $.receipts.delivered)
          : t(($) => $.receipts.sent);
  return (
    <span
      className={`delivery-status ${read ? "delivery-status--read" : ""}`}
      title={label}
      role="img"
      aria-label={label}
    >
      <HugeiconsIcon
        icon={complete ? TickDouble01Icon : Tick01Icon}
        size={15}
        aria-hidden="true"
      />
      {delivery?.group && known && (
        <span>{delivery.read_count || delivery.delivered_count}</span>
      )}
    </span>
  );
}
