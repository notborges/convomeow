import {
  ArrowDown01Icon,
  ArrowLeft01Icon,
  Chat01Icon,
  InformationCircleIcon,
} from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { keys } from "../../api/queries";
import type { Account, Message } from "../../api/types";
import { LiveAvatar } from "../../app/LiveAvatar";
import i18n from "../../i18n";
import { formatDate } from "../../i18n/format";
import { Button, IconButton } from "../../ui/Button";
import { EmptyState } from "../../ui/EmptyState";
import { Loading } from "../../ui/Loading";
import { motionTiming } from "../../ui/motion";
import { ProviderBadge } from "../../ui/ProviderBadge";
import { Composer } from "./Composer";
import { ConversationDetails } from "./ConversationDetails";
import { MessageBubble } from "./MessageBubble";

function messageDay(value: string): string {
  const date = new Date(value);
  const today = new Date();
  if (date.toDateString() === today.toDateString())
    return i18n.t(($) => $.thread.today);
  const yesterday = new Date(today);
  yesterday.setDate(today.getDate() - 1);
  if (date.toDateString() === yesterday.toDateString())
    return i18n.t(($) => $.thread.yesterday);
  return formatDate(value, {
    weekday: "long",
    month: "long",
    day: "numeric",
  });
}

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
  const [anchor, setAnchor] = useState("");
  const [jumpTarget, setJumpTarget] = useState("");
  const [highlight, setHighlight] = useState("");
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [awayFromBottom, setAwayFromBottom] = useState(false);
  const conversation = useQuery({
    queryKey: keys.conversation(conversationID),
    meta: { accountID: account.id },
    queryFn: () => api.conversation(conversationID),
  });
  const messages = useInfiniteQuery({
    queryKey: [...keys.messages(conversationID), { around: anchor }],
    meta: { accountID: account.id },
    queryFn: ({ pageParam }) =>
      api.messages(conversationID, pageParam.cursor, pageParam),
    initialPageParam: { around: anchor } as {
      around?: string;
      cursor?: string;
      after?: string;
    },
    getNextPageParam: (page) =>
      page.next_cursor ? { cursor: page.next_cursor } : undefined,
    getPreviousPageParam: (page) =>
      page.previous_cursor ? { after: page.previous_cursor } : undefined,
  });
  const scroll = useRef<HTMLDivElement>(null);
  const nearBottom = useRef(true);
  const lastContentHeight = useRef(0);
  const priorHeight = useRef<number | undefined>(undefined);
  const items = messages.data?.pages.flatMap((page) => page.items) ?? [];
  const ordered = [...items].reverse();
  const newestID = items[0]?.id;

  useEffect(() => {
    if (!anchor && nearBottom.current && scroll.current)
      scroll.current.scrollTop = scroll.current.scrollHeight;
  }, [newestID, anchor]);

  useLayoutEffect(() => {
    if (priorHeight.current !== undefined && scroll.current) {
      scroll.current.scrollTop +=
        scroll.current.scrollHeight - priorHeight.current;
      priorHeight.current = undefined;
    }
  }, [messages.data?.pages.length]);

  useLayoutEffect(() => {
    const area = scroll.current;
    const flow = area?.querySelector(".message-flow");
    if (!area || !flow) return;
    const observer = new ResizeObserver(() => {
      if (nearBottom.current) area.scrollTop = area.scrollHeight;
      lastContentHeight.current = area.scrollHeight;
    });
    observer.observe(flow);
    return () => observer.disconnect();
  }, []);

  function jumpToMessage(id: string) {
    nearBottom.current = false;
    setJumpTarget(id);
    if (!items.some((message) => message.id === id)) setAnchor(id);
  }

  useLayoutEffect(() => {
    if (!jumpTarget) return;
    const target = document.getElementById(`message-${jumpTarget}`);
    if (!target) return;
    target.scrollIntoView({ block: "center" });
    target.focus({ preventScroll: true });
    setHighlight(jumpTarget);
    setJumpTarget("");
    setAwayFromBottom(true);
  }, [jumpTarget, items]);

  useEffect(() => {
    if (!highlight) return;
    const timer = window.setTimeout(() => setHighlight(""), 1800);
    return () => window.clearTimeout(timer);
  }, [highlight]);

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

  function loadOlder() {
    if (scroll.current) priorHeight.current = scroll.current.scrollHeight;
    messages.fetchNextPage();
  }

  function onScroll() {
    const area = scroll.current;
    if (area) {
      if (nearBottom.current && area.scrollHeight !== lastContentHeight.current)
        area.scrollTop = area.scrollHeight;
      lastContentHeight.current = area.scrollHeight;
      nearBottom.current =
        area.scrollHeight - area.scrollTop - area.clientHeight < 100;
      setAwayFromBottom(!nearBottom.current);
    }
  }

  let lastDay = "";
  return (
    <motion.main
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={motionTiming.feedback}
      className="thread"
      aria-label={t(($) => $.thread.conversation)}
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
                <ProviderBadge provider={account.provider} />
                <span aria-hidden="true">·</span>
                {conversation.data.kind === "group"
                  ? conversation.data.description || t(($) => $.thread.group)
                  : conversation.data.contact?.phone ||
                    t(($) => $.thread.direct)}
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
      <div className="thread-body">
        <div className="thread-messages" ref={scroll} onScroll={onScroll}>
          <div className="message-flow">
            {anchor && (
              <Button
                className="load-more"
                onClick={() => {
                  setAnchor("");
                  setJumpTarget("");
                  nearBottom.current = true;
                }}
              >
                {t(($) => $.thread.latest)}
              </Button>
            )}
            {messages.hasNextPage && (
              <Button
                className="load-more load-more--messages"
                type="button"
                onClick={loadOlder}
                disabled={messages.isFetchingNextPage}
              >
                {messages.isFetchingNextPage
                  ? t(($) => $.chats.loadingMore)
                  : t(($) => $.thread.older)}
              </Button>
            )}
            {messages.isPending ? (
              <Loading label={t(($) => $.thread.loading)} />
            ) : messages.isError ? (
              <EmptyState title={t(($) => $.thread.error)}>
                <Button variant="text" onClick={() => messages.refetch()}>
                  {t(($) => $.common.retry)}
                </Button>
              </EmptyState>
            ) : ordered.length === 0 ? (
              <EmptyState
                icon={<HugeiconsIcon icon={Chat01Icon} size={28} />}
                title={t(($) => $.thread.hello)}
                description={t(($) => $.thread.first)}
              />
            ) : (
              ordered.map((message, index) => {
                const previous = ordered[index - 1];
                const grouped =
                  !!previous &&
                  previous.direction === message.direction &&
                  previous.sender_id === message.sender_id &&
                  new Date(message.occurred_at).getTime() -
                    new Date(previous.occurred_at).getTime() <
                    5 * 60 * 1000;
                const day = new Date(message.occurred_at).toDateString();
                const showDay = day !== lastDay;
                lastDay = day;
                return (
                  <div
                    key={message.id}
                    className={
                      highlight === message.id ? "reply-target" : undefined
                    }
                  >
                    {showDay && (
                      <div className="day-label">
                        {messageDay(message.occurred_at)}
                      </div>
                    )}
                    <MessageBubble
                      message={message}
                      onReply={(message) => setReply({ ...message })}
                      onJump={jumpToMessage}
                      replySender={
                        message.reply ? replySender(message.reply) : undefined
                      }
                      grouped={grouped && !showDay}
                      showSender={conversation.data?.kind === "group"}
                    />
                  </div>
                );
              })
            )}
            {messages.hasPreviousPage && (
              <Button
                className="load-more"
                disabled={messages.isFetchingPreviousPage}
                onClick={() => messages.fetchPreviousPage()}
              >
                {t(($) => $.reply.newer)}
              </Button>
            )}
          </div>
        </div>
        <AnimatePresence>
          {awayFromBottom && !anchor && (
            <motion.div
              className="jump-latest"
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: 8 }}
              transition={motionTiming.feedback}
            >
              <Button
                onClick={() => {
                  setAnchor("");
                  setJumpTarget("");
                  if (scroll.current)
                    scroll.current.scrollTop = scroll.current.scrollHeight;
                  nearBottom.current = true;
                  setAwayFromBottom(false);
                }}
              >
                <HugeiconsIcon icon={ArrowDown01Icon} size={18} />
                {t(($) => $.thread.latest)}
              </Button>
            </motion.div>
          )}
        </AnimatePresence>
      </div>
      <Composer
        accountID={account.id}
        conversationID={conversationID}
        connected={account.state === "connected"}
        onSent={() => {
          setAnchor("");
          setJumpTarget("");
          nearBottom.current = true;
        }}
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
