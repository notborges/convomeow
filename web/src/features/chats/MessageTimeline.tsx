import { ArrowDown01Icon, Chat01Icon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useQueryClient } from "@tanstack/react-query";
import {
  type Ref,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import { type ListRange, Virtuoso, type VirtuosoHandle } from "react-virtuoso";
import type { Message } from "../../api/types";
import { formatDate } from "../../i18n/format";
import { Button } from "../../ui/Button";
import { EmptyState } from "../../ui/EmptyState";
import { Loading } from "../../ui/Loading";
import { MessageBubble } from "./MessageBubble";
import {
  initialMessageIndex,
  messageGrouping,
  shiftedMessageIndex,
} from "./messageWindow";
import { useInitialBottomAnchor } from "./useInitialBottomAnchor";
import { messageWindowKey, useMessageWindow } from "./useMessageWindow";
import { useReadingAnchor } from "./useReadingAnchor";

export interface MessageTimelineHandle {
  latest: () => void;
}

type Props = {
  accountID: string;
  conversationID: string;
  showSender: boolean;
  replySender: (message: {
    sender_id?: string;
    direction?: string;
  }) => string | undefined;
  onReply: (message: Message) => void;
  ref?: Ref<MessageTimelineHandle>;
};

export function MessageTimeline({ ref, ...props }: Props) {
  const client = useQueryClient();
  const [position, setPosition] = useState({ around: "", revision: 0 });
  const [target, setTarget] = useState("");
  const [latestRequest, setLatestRequest] = useState(0);
  const query = useMessageWindow(
    props.accountID,
    props.conversationID,
    position.around,
  );
  const { t } = useTranslation();

  function latest() {
    setTarget("");
    if (!position.around && !query.hasPreviousPage) {
      setLatestRequest((current) => current + 1);
      return;
    }
    void client.resetQueries({
      queryKey: messageWindowKey(props.conversationID, ""),
      exact: true,
    });
    setPosition((current) => ({ around: "", revision: current.revision + 1 }));
  }
  useImperativeHandle(ref, () => ({ latest }));

  function jump(id: string) {
    setTarget(id);
    if (!query.items.some((message) => message.id === id))
      setPosition((current) => ({
        around: id,
        revision: current.revision + 1,
      }));
  }

  return (
    <div className="thread-body">
      {query.isPending ? (
        <Loading label={t(($) => $.thread.loading)} />
      ) : !query.data && query.isError ? (
        <EmptyState title={t(($) => $.thread.error)}>
          <Button variant="text" onClick={() => query.refetch()}>
            {t(($) => $.common.retry)}
          </Button>
          {position.around && (
            <Button variant="text" onClick={latest}>
              {t(($) => $.thread.latest)}
            </Button>
          )}
        </EmptyState>
      ) : query.items.length === 0 ? (
        <EmptyState
          icon={<HugeiconsIcon icon={Chat01Icon} size={28} />}
          title={t(($) => $.thread.hello)}
          description={t(($) => $.thread.first)}
        />
      ) : (
        <VirtualMessages
          key={`${position.around}:${position.revision}`}
          {...props}
          query={query}
          target={target}
          latestRequest={latestRequest}
          onJump={jump}
          onJumpDone={() => setTarget("")}
          onLatest={latest}
        />
      )}
    </div>
  );
}

type EdgeState = { loading: boolean; failed: boolean; retry: () => void };
type ListContext = { older: EdgeState; newer: EdgeState };

function HistoryEdge({ state }: { state: EdgeState }) {
  const { t } = useTranslation();
  return (
    <div className="history-edge">
      {state.loading ? (
        <span role="status">{t(($) => $.thread.loading)}</span>
      ) : state.failed ? (
        <div role="alert">
          <span>{t(($) => $.thread.historyError)}</span>
          <Button variant="text" onClick={state.retry}>
            {t(($) => $.common.retry)}
          </Button>
        </div>
      ) : null}
    </div>
  );
}

const listComponents = {
  Header: ({ context }: { context?: ListContext }) =>
    context && <HistoryEdge state={context.older} />,
  Footer: ({ context }: { context?: ListContext }) =>
    context && <HistoryEdge state={context.newer} />,
};

function VirtualMessages({
  query,
  target,
  latestRequest,
  onJump,
  onJumpDone,
  onLatest,
  showSender,
  replySender,
  onReply,
}: Omit<Props, "ref"> & {
  query: ReturnType<typeof useMessageWindow>;
  target: string;
  latestRequest: number;
  onJump: (id: string) => void;
  onJumpDone: () => void;
  onLatest: () => void;
}) {
  const { t } = useTranslation();
  const list = useRef<VirtuosoHandle>(null);
  const loading = useRef(false);
  const [range, setRange] = useState<ListRange>();
  const [atBottom, setAtBottom] = useState(!target);
  const [scrolling, setScrolling] = useState(false);
  const [highlight, setHighlight] = useState("");
  const [scroller, setScroller] = useState<HTMLElement | null>(null);
  const scrollerRef = useCallback((element: HTMLElement | Window | null) => {
    setScroller(element instanceof HTMLElement ? element : null);
  }, []);
  const items = query.items;
  const [window, setWindow] = useState({ items, first: initialMessageIndex });
  let first = window.first;
  if (window.items !== items) {
    first = shiftedMessageIndex(window.items, items, window.first);
    setWindow({ items, first });
  }
  const observeRow = useReadingAnchor(
    scroller,
    !atBottom && !target && !scrolling,
    `${first}:${items.length}`,
  );
  const settleInitialBottom = useInitialBottomAnchor(
    list,
    scroller,
    !target && !query.hasPreviousPage,
  );
  const [initialPosition] = useState(() => {
    const index = target
      ? items.findIndex((item) => item.id === target)
      : items.length - 1;
    return {
      index: Math.max(0, index),
      align: target ? ("center" as const) : ("end" as const),
    };
  });

  useEffect(() => {
    if (latestRequest)
      list.current?.scrollToIndex({ index: "LAST", align: "end" });
  }, [latestRequest]);

  async function load(direction: "older" | "newer") {
    if (loading.current || query.isFetching) return;
    if (direction === "older" ? !query.hasNextPage : !query.hasPreviousPage)
      return;
    loading.current = true;
    try {
      if (direction === "older")
        await query.fetchNextPage({ cancelRefetch: false });
      else await query.fetchPreviousPage({ cancelRefetch: false });
    } finally {
      loading.current = false;
    }
  }

  useEffect(() => {
    if (!range || target || query.isFetching) return;
    if (
      range.startIndex <= first + 12 &&
      query.hasNextPage &&
      !query.isFetchNextPageError
    )
      void load("older");
    else if (
      range.endIndex >= first + items.length - 13 &&
      query.hasPreviousPage &&
      !query.isFetchPreviousPageError
    )
      void load("newer");
  }, [
    range,
    target,
    first,
    items.length,
    query.isFetching,
    query.hasNextPage,
    query.hasPreviousPage,
    query.isFetchNextPageError,
    query.isFetchPreviousPageError,
  ]);

  useEffect(() => {
    if (!target) return;
    const index = items.findIndex((message) => message.id === target);
    if (index < 0) return;
    list.current?.scrollIntoView({
      index,
      align: "center",
      behavior: "auto",
    });
  }, [target, items, first]);

  useEffect(() => {
    if (!target) return;
    let frame: number;
    function focusTarget() {
      const element = document.getElementById(`message-${target}`);
      if (!element) return;
      // Virtuoso measures new rows while hidden before positioning them.
      if (!element.checkVisibility({ visibilityProperty: true })) {
        frame = requestAnimationFrame(focusTarget);
        return;
      }
      element.focus({ preventScroll: true });
      setHighlight(target);
      onJumpDone();
    }
    frame = requestAnimationFrame(focusTarget);
    return () => cancelAnimationFrame(frame);
  }, [range, target]);

  useEffect(() => {
    if (!highlight) return;
    const timer = setTimeout(() => setHighlight(""), 1800);
    return () => clearTimeout(timer);
  }, [highlight]);

  const rows = useMemo(
    () =>
      items.map((message, index) => ({
        message,
        ...messageGrouping(message, items[index - 1]),
      })),
    [items],
  );

  function dayLabel(value: string) {
    const date = new Date(value);
    const today = new Date();
    if (date.toDateString() === today.toDateString())
      return t(($) => $.thread.today);
    today.setDate(today.getDate() - 1);
    if (date.toDateString() === today.toDateString())
      return t(($) => $.thread.yesterday);
    return formatDate(value, {
      weekday: "long",
      month: "long",
      day: "numeric",
    });
  }

  return (
    <>
      <Virtuoso
        ref={list}
        scrollerRef={scrollerRef}
        className="thread-messages"
        data={rows}
        firstItemIndex={first}
        initialTopMostItemIndex={initialPosition}
        computeItemKey={(_, row) => row.message.id}
        defaultItemHeight={100}
        alignToBottom
        followOutput={query.hasPreviousPage || target ? false : "auto"}
        scrollIntoViewOnChange={() =>
          atBottom && !query.hasPreviousPage && !target && !scrolling
            ? {
                index: items.length - 1,
                align: "end",
                behavior: "auto",
              }
            : false
        }
        atBottomThreshold={80}
        atBottomStateChange={setAtBottom}
        isScrolling={setScrolling}
        increaseViewportBy={{ top: 600, bottom: 400 }}
        rangeChanged={setRange}
        totalListHeightChanged={settleInitialBottom}
        components={listComponents}
        context={{
          older: {
            loading: query.isFetchingNextPage,
            failed: query.isFetchNextPageError,
            retry: () => void load("older"),
          },
          newer: {
            loading: query.isFetchingPreviousPage,
            failed: query.isFetchPreviousPageError,
            retry: () => void load("newer"),
          },
        }}
        itemContent={(_, { message, showDay, grouped }) => (
          <div
            ref={observeRow}
            data-message-id={message.id}
            className={`message-row ${highlight === message.id ? "reply-target" : ""}`}
          >
            {showDay && (
              <div className="day-label">{dayLabel(message.occurred_at)}</div>
            )}
            <MessageBubble
              message={message}
              grouped={grouped}
              showSender={showSender}
              onReply={onReply}
              onJump={onJump}
              replySender={
                message.reply ? replySender(message.reply) : undefined
              }
            />
          </div>
        )}
      />
      {(!atBottom || query.hasPreviousPage) && (
        <div className="jump-latest">
          <Button
            onClick={() => {
              if (query.hasPreviousPage) onLatest();
              else list.current?.scrollToIndex({ index: "LAST", align: "end" });
            }}
          >
            <HugeiconsIcon icon={ArrowDown01Icon} size={18} />
            {t(($) => $.thread.latest)}
          </Button>
        </div>
      )}
    </>
  );
}
