import {
  ArrowDown01Icon,
  ArrowTurnBackwardIcon,
  Copy01Icon,
  Tick01Icon,
} from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { Message } from "../../api/types";
import { formatDate, kindLabel, stateLabel } from "../../i18n/format";
import { ActionMenu } from "../../ui/ActionMenu";
import { AttachmentView } from "./AttachmentView";
import { ReplyPreview } from "./ReplyPreview";

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
}: {
  message: Message;
  showSender: boolean;
  grouped: boolean;
  onReply?: (message: Message) => void;
  onJump?: (id: string) => void;
  replySender?: string;
}) {
  const { t } = useTranslation();
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">(
    "idle",
  );
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
      id={`message-${message.id}`}
      tabIndex={-1}
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
        {onReply &&
          (message.state === "sent" || message.state === "received") && (
            <ActionMenu
              className="message-actions"
              label={t(($) => $.message.actions)}
              align="start"
              side={outgoing ? "left" : "right"}
              onOpen={() => setCopyState("idle")}
              triggerIcon={<HugeiconsIcon icon={ArrowDown01Icon} size={19} />}
              actions={[
                {
                  id: "reply",
                  label: t(($) => $.reply.action),
                  icon: (
                    <HugeiconsIcon icon={ArrowTurnBackwardIcon} size={20} />
                  ),
                  movesFocus: true,
                  onSelect: () => onReply(message),
                },
                ...(content
                  ? [
                      {
                        id: "copy",
                        label:
                          copyState === "copied"
                            ? t(($) => $.message.copied)
                            : copyState === "failed"
                              ? t(($) => $.message.copyFailed)
                              : t(($) => $.message.copy),
                        icon: <HugeiconsIcon icon={Copy01Icon} size={20} />,
                        closeOnSelect: false,
                        onSelect: async () => {
                          try {
                            await navigator.clipboard.writeText(content);
                            setCopyState("copied");
                          } catch {
                            setCopyState("failed");
                          }
                        },
                      },
                    ]
                  : []),
              ]}
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
