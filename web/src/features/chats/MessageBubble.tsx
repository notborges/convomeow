import { Tick01Icon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { Message } from "../../api/types";
import { formatDate, kindLabel, stateLabel } from "../../i18n/format";
import { AttachmentView } from "./AttachmentView";

function messageTime(value: string): string {
  return formatDate(value, {
    hour: "numeric",
    minute: "2-digit",
  });
}

export function MessageBubble({
  message,
  showSender,
  grouped,
}: {
  message: Message;
  showSender: boolean;
  grouped: boolean;
}) {
  const { t } = useTranslation();
  const firstImage = message.attachments?.find(
    (item) => item.kind === "image" || item.kind === "sticker",
  );
  const [measured, setMeasured] = useState<{ width: number; height: number }>();
  const dimensions = measured ?? firstImage;
  const ratio =
    dimensions?.width && dimensions?.height
      ? dimensions.width / dimensions.height
      : undefined;

  const outgoing = message.direction === "outbound";
  const content = message.content?.text || message.content?.caption;
  const state =
    outgoing && message.state !== "sent" ? stateLabel(message.state) : "";
  return (
    <article
      className={`message ${message.attachments?.length ? "message--media" : ""} ${outgoing ? "message--outgoing" : ""} ${grouped ? "message--grouped" : ""}`}
    >
      <div
        className="message__bubble"
        style={
          ratio
            ? { width: `min(84%, ${Math.min(360, 400 * ratio + 8)}px)` }
            : undefined
        }
      >
        {showSender && message.sender_id && !outgoing && (
          <span className="message__sender">
            {message.sender_id.split("@")[0]}
          </span>
        )}
        {message.attachments?.map((attachment) => (
          <AttachmentView
            key={attachment.id}
            attachment={attachment}
            caption={content}
            onDimensions={
              attachment.id === firstImage?.id ? setMeasured : undefined
            }
          />
        ))}
        {content && <p>{content}</p>}
        {!content && !message.attachments?.length && (
          <p className="message__type">{kindLabel(message.kind)}</p>
        )}
        <div className="message__meta">
          <time dateTime={message.occurred_at}>
            {messageTime(message.occurred_at)}
          </time>
          {outgoing && message.state === "sent" ? (
            <HugeiconsIcon
              icon={Tick01Icon}
              size={13}
              aria-label={t(($) => $.message.sent)}
            />
          ) : (
            state && <span>{state}</span>
          )}
        </div>
      </div>
    </article>
  );
}
