import {
  ArrowLeft01Icon,
  InformationCircleIcon,
} from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useQuery } from "@tanstack/react-query";
import { motion } from "motion/react";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { keys } from "../../api/queries";
import type { Account, Message } from "../../api/types";
import { LiveAvatar } from "../../app/LiveAvatar";
import { IconButton } from "../../ui/Button";
import { motionTiming } from "../../ui/motion";
import { ProviderBadge } from "../../ui/ProviderBadge";
import { ChatPresenceIndicator } from "../presence/PresenceIndicator";
import { Composer } from "./Composer";
import { ConversationDetails } from "./ConversationDetails";
import { MessageTimeline, type MessageTimelineHandle } from "./MessageTimeline";

export function ChatThread({
  account,
  conversationID,
  onBack,
}: {
  account: Account;
  conversationID: string;
  onBack: () => void;
}) {
  const { t } = useTranslation();

  const [reply, setReply] = useState<Message>();
  const [detailsOpen, setDetailsOpen] = useState(false);
  const timeline = useRef<MessageTimelineHandle>(null);
  const composerInput = useRef<HTMLTextAreaElement>(null);
  const conversation = useQuery({
    queryKey: keys.conversation(conversationID),
    meta: { accountID: account.id },
    queryFn: () => api.conversation(conversationID),
  });
  function replySender(message: { sender_id?: string; direction?: string }) {
    if (
      message.direction === "outbound" ||
      message.sender_id === account.provider_identity
    )
      return t(($) => $.reply.you);
    if (conversation.data?.kind === "direct")
      return conversation.data.display_name;
    return message.sender_id?.split("@")[0];
  }

  return (
    <motion.main
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={motionTiming.feedback}
      className="thread"
      aria-label={t(($) => $.thread.conversation)}
      onClick={(event) => {
        if (
          event.defaultPrevented ||
          event.detail !== 1 ||
          !(event.target instanceof Element) ||
          !event.currentTarget.contains(event.target) ||
          event.target.closest(
            'button, a, input, textarea, select, label, audio, video, [role="button"], [role="menu"], [role="menuitem"], [role="dialog"], [contenteditable="true"]',
          ) ||
          window.getSelection()?.toString()
        )
          return;
        composerInput.current?.focus({ preventScroll: true });
      }}
    >
      <header className="thread-head">
        <IconButton
          className="thread-back"
          label={t(($) => $.thread.back)}
          onClick={onBack}
        >
          <HugeiconsIcon icon={ArrowLeft01Icon} size={21} />
        </IconButton>
        {conversation.data ? (
          <>
            <LiveAvatar
              accountID={account.id}
              name={conversation.data.display_name}
              url={conversation.data.avatar_url}
              size="small"
            />
            <div className="thread-head__title">
              <strong>{conversation.data.display_name}</strong>
              <span className="thread-head__subtitle">
                <ChatPresenceIndicator
                  conversationID={conversationID}
                  group={conversation.data.kind === "group"}
                />
                <span className="thread-head__default-subtitle">
                  <ProviderBadge provider={account.provider} />
                  <span aria-hidden="true">·</span>
                  {conversation.data.kind === "group"
                    ? conversation.data.description || t(($) => $.thread.group)
                    : conversation.data.contact?.phone ||
                      t(($) => $.thread.direct)}
                </span>
              </span>
            </div>
          </>
        ) : (
          <div className="thread-head__title">
            <strong>
              {conversation.isError
                ? t(($) => $.thread.unavailable)
                : t(($) => $.thread.loadingChat)}
            </strong>
          </div>
        )}
        <div className="thread-head__actions">
          <span className="thread-head__context">{account.label}</span>
          <IconButton
            label={t(($) => $.thread.details)}
            disabled={!conversation.data}
            onClick={() => setDetailsOpen(true)}
          >
            <HugeiconsIcon icon={InformationCircleIcon} size={22} />
          </IconButton>
        </div>
      </header>
      <MessageTimeline
        ref={timeline}
        accountID={account.id}
        conversationID={conversationID}
        showSender={conversation.data?.kind === "group"}
        replySender={replySender}
        onReply={(message) => setReply({ ...message })}
      />
      <Composer
        inputRef={composerInput}
        accountID={account.id}
        conversationID={conversationID}
        connected={account.state === "connected"}
        capabilities={account.capabilities}
        onSent={() => timeline.current?.latest()}
        reply={reply}
        replySender={reply ? replySender(reply) : undefined}
        onClearReply={(id) =>
          setReply((current) =>
            !id || current?.id === id ? undefined : current,
          )
        }
      />
      {detailsOpen && conversation.data && (
        <ConversationDetails
          conversation={conversation.data}
          account={account}
          onClose={() => setDetailsOpen(false)}
        />
      )}
    </motion.main>
  );
}
