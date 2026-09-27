import { Delete02Icon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError } from "../../api/client";
import type { Message } from "../../api/types";
import { errorKey } from "../../i18n/errors";
import { formatDate, kindLabel } from "../../i18n/format";
import { Button } from "../../ui/Button";
import { AttachmentView } from "./AttachmentView";
import { DeliveryStatus } from "./DeliveryStatus";
import { MessageActions } from "./MessageActions";
import { MessageChangeDialog } from "./MessageChangeDialog";
import { MessageHistory } from "./MessageHistory";
import { MessageInfo } from "./MessageInfo";
import { MessageReactions } from "./MessageReactions";
import { ReplyPreview } from "./ReplyPreview";
import { useMessageReaction } from "./useMessageReaction";

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
  onReply,
  onJump,
  replySender,
  onReact,
}: {
  message: Message;
  showSender: boolean;
  grouped: boolean;
  onReply?: (message: Message) => void;
  onJump?: (id: string) => void;
  replySender?: string;
  onReact?: (emoji: string) => Promise<void>;
}) {
  const { t } = useTranslation();
  const [change, setChange] = useState<"edit" | "revoke">();
  const [historyOpen, setHistoryOpen] = useState(false);
  const [infoOpen, setInfoOpen] = useState(false);
  const reaction = useMessageReaction(message, onReact);
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
  return (
    <article
      id={`message-${message.id}`}
      tabIndex={-1}
      className={`message ${message.reply ? "message--reply" : ""} ${message.deleted_at ? "message--deleted" : ""} ${message.attachments?.length ? "message--media" : ""} ${outgoing ? "message--outgoing" : ""} ${grouped ? "message--grouped" : ""}`}
    >
      <div
        className="message__bubble"
        style={
          ratio
            ? { width: `min(84%, ${Math.min(360, 400 * ratio + 8)}px)` }
            : undefined
        }
      >
        {onReply &&
          (message.state === "sent" ||
            message.state === "received" ||
            (outgoing &&
              message.delivery &&
              message.delivery.state !== "unknown")) && (
            <MessageActions
              message={message}
              onReply={onReply}
              onInfo={() => setInfoOpen(true)}
              onHistory={() => setHistoryOpen(true)}
              onReact={(emoji) => reaction.mutate(emoji)}
              busy={reaction.isPending}
              onChange={setChange}
            />
          )}
        {showSender && message.sender_id && !outgoing && (
          <span className="message__sender">
            {message.sender_id.split("@")[0]}
          </span>
        )}
        {message.reply && (
          <ReplyPreview
            reply={message.reply}
            sender={replySender}
            onJump={onJump}
          />
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
        {!message.deleted_at && !content && !message.attachments?.length && (
          <p className="message__type">{kindLabel(message.kind)}</p>
        )}
        <div className="message__meta">
          {message.deleted_at && (
            <Button
              variant="ghost"
              className="message__archive"
              aria-label={t(($) => $.messageChanges.savedHistory)}
              title={t(($) => $.messageChanges.savedHistory)}
              onClick={() => setHistoryOpen(true)}
            >
              <HugeiconsIcon icon={Delete02Icon} size={14} aria-hidden="true" />
              {t(($) => $.messageChanges.savedCopy)}
            </Button>
          )}
          {message.edited_at && !message.deleted_at && (
            <span>{t(($) => $.messageChanges.edited)}</span>
          )}
          <time dateTime={message.occurred_at}>
            {messageTime(message.occurred_at)}
          </time>
          {outgoing && !message.deleted_at && (
            <DeliveryStatus message={message} />
          )}
        </div>
        <MessageReactions message={message} />
      </div>
      {reaction.isPending && (
        <span className="sr-only" role="status">
          {t(($) => $.reactions.sending)}
        </span>
      )}
      {reaction.isError && (
        <div className="message-reaction-error" role="alert">
          <span>
            {reaction.error instanceof ApiError &&
            reaction.error.code !== "reaction_unconfirmed"
              ? t(($) => $.errors[errorKey(reaction.error)])
              : t(($) => $.reactions.failed)}
          </span>
          <Button variant="text" onClick={() => reaction.refresh()}>
            {t(($) => $.reactions.refresh)}
          </Button>
        </div>
      )}
      {change && (
        <MessageChangeDialog
          message={message}
          kind={change}
          onClose={() => setChange(undefined)}
        />
      )}
      {historyOpen && (
        <MessageHistory
          message={message}
          onClose={() => setHistoryOpen(false)}
        />
      )}
      {infoOpen && (
        <MessageInfo message={message} onClose={() => setInfoOpen(false)} />
      )}
    </article>
  );
}
