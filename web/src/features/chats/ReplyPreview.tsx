import { useTranslation } from "react-i18next";
import type { Reply } from "../../api/types";
import { kindLabel } from "../../i18n/format";

export function ReplyPreview({
  reply,
  sender,
  onJump,
}: {
  reply: Reply;
  sender?: string;
  onJump?: (id: string) => void;
}) {
  const { t } = useTranslation();
  const content = (
    <>
      <strong>
        {sender ||
          reply.sender_id?.split("@")[0] ||
          t(($) => $.reply.unknownSender)}
      </strong>
      <span>
        {reply.text ||
          (reply.deleted
            ? t(($) => $.messageChanges.deleted)
            : kindLabel(reply.kind))}
      </span>
    </>
  );
  if (reply.message_id && onJump) {
    const id = reply.message_id;
    return (
      <button
        type="button"
        className="reply-preview"
        aria-label={t(($) => $.reply.jump)}
        onClick={() => onJump(id)}
      >
        {content}
      </button>
    );
  }
  return (
    <div
      className="reply-preview"
      title={!reply.message_id ? t(($) => $.reply.unavailable) : undefined}
    >
      {content}
    </div>
  );
}
