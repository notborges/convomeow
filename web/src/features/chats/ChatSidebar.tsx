import {
  Chat01Icon,
  ChatAdd01Icon,
  SmartphoneIcon,
} from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { keys } from "../../api/queries";
import type { Account, Conversation, Message } from "../../api/types";
import { LiveAvatar } from "../../app/LiveAvatar";
import i18n from "../../i18n";
import { errorKey } from "../../i18n/errors";
import { formatDate, kindLabel } from "../../i18n/format";
import { Button, IconButton } from "../../ui/Button";
import { EmptyState } from "../../ui/EmptyState";
import { SearchField, TextField } from "../../ui/Field";
import { Loading } from "../../ui/Loading";
import { ProviderBadge } from "../../ui/ProviderBadge";
import { SegmentedControl } from "../../ui/SegmentedControl";
import { StatusBadge } from "../../ui/StatusBadge";

interface Props {
  account: Account;
  activeConversationID?: string;
  onOpen: (id: string) => void;
  onPair: () => void;
}

function preview(message?: Message): string {
  if (!message) return i18n.t(($) => $.chats.noMessages);
  const content =
    message.kind === "text"
      ? message.content.text || i18n.t(($) => $.composer.message)
      : message.content.caption || kindLabel(message.kind);
  return message.direction === "outbound"
    ? i18n.t(($) => $.chats.you, { message: content })
    : content;
}

function timeLabel(value?: string): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const today = new Date();
  if (date.toDateString() === today.toDateString())
    return formatDate(date, {
      hour: "numeric",
      minute: "2-digit",
    });
  return formatDate(date, {
    month: "short",
    day: "numeric",
  });
}

export function ChatSidebar({
  account,
  activeConversationID,
  onOpen,
  onPair,
}: Props) {
  const { t } = useTranslation();

  const [tab, setTab] = useState<"chats" | "contacts">("chats");
  const [search, setSearch] = useState("");
  const [phone, setPhone] = useState("");
  const [showPhone, setShowPhone] = useState(false);
  const queryClient = useQueryClient();
  const conversations = useInfiniteQuery({
    queryKey: keys.conversations(account.id),
    queryFn: ({ pageParam }) => api.conversations(account.id, pageParam),
    initialPageParam: "",
    getNextPageParam: (page) => page.next_cursor,
  });
  const contacts = useInfiniteQuery({
    queryKey: keys.contacts(account.id, search),
    queryFn: ({ pageParam }) => api.contacts(account.id, search, pageParam),
    initialPageParam: "",
    getNextPageParam: (page) => page.next_cursor,
    enabled: tab === "contacts" && account.state === "connected",
    staleTime: 15000,
  });
  const create = useMutation({
    mutationFn: ({
      type,
      value,
    }: {
      type: "contact" | "phone_number";
      value: string;
    }) => api.createConversation(account.id, type, value),
    onSuccess: (conversation) => {
      queryClient.invalidateQueries({
        queryKey: keys.conversations(account.id),
      });
      setShowPhone(false);
      setPhone("");
      onOpen(conversation.id);
    },
  });
  const chats = conversations.data?.pages.flatMap((page) => page.items) ?? [];
  const filtered =
    search && tab === "chats"
      ? chats.filter((chat) =>
          chat.display_name.toLowerCase().includes(search.toLowerCase()),
        )
      : chats;

  function startPhone(event: FormEvent) {
    event.preventDefault();
    if (phone.trim())
      create.mutate({ type: "phone_number", value: phone.trim() });
  }

  return (
    <aside
      className="sidebar"
      aria-label={t(($) => $.chats.label, { name: account.label })}
    >
      <header className="sidebar-head">
        <div className="sidebar-head__top">
          <div>
            <h1>{t(($) => $.chats.title)}</h1>
          </div>
          <IconButton
            variant="primary"
            label={t(($) => $.chats.new)}
            disabled={account.state !== "connected"}
            onClick={() => {
              setTab("contacts");
              setSearch("");
            }}
          >
            <HugeiconsIcon icon={ChatAdd01Icon} size={22} />
          </IconButton>
        </div>
        <SearchField
          label={
            tab === "contacts"
              ? t(($) => $.chats.searchContacts)
              : t(($) => $.chats.searchLoaded)
          }
          value={search}
          onChange={setSearch}
        />
        <SegmentedControl
          label={t(($) => $.chats.browse)}
          value={tab}
          options={[
            { value: "chats", label: t(($) => $.chats.chats) },
            { value: "contacts", label: t(($) => $.chats.contacts) },
          ]}
          onChange={(value) => {
            setTab(value);
            setSearch("");
          }}
        />
      </header>
      {tab === "chats" ? (
        <div className="sidebar-list">
          {conversations.isPending ? (
            <Loading label={t(($) => $.chats.loading)} />
          ) : conversations.isError ? (
            <EmptyState title={t(($) => $.chats.loadError)}>
              <Button variant="text" onClick={() => conversations.refetch()}>
                {t(($) => $.common.retry)}
              </Button>
            </EmptyState>
          ) : filtered.length === 0 ? (
            <EmptyState
              icon={<HugeiconsIcon icon={Chat01Icon} size={28} />}
              title={
                search ? t(($) => $.chats.noMatches) : t(($) => $.chats.empty)
              }
              description={
                search
                  ? t(($) => $.chats.trySearch)
                  : t(($) => $.chats.sayHello)
              }
            >
              {!search && (
                <Button variant="text" onClick={() => setTab("contacts")}>
                  {t(($) => $.chats.browseContacts)}
                </Button>
              )}
            </EmptyState>
          ) : (
            filtered.map((chat: Conversation) => (
              <button
                key={chat.id}
                type="button"
                className={`chat-row ${activeConversationID === chat.id ? "chat-row--active" : ""}`}
                aria-current={
                  activeConversationID === chat.id ? "page" : undefined
                }
                onClick={() => onOpen(chat.id)}
              >
                <LiveAvatar
                  accountID={account.id}
                  name={chat.display_name}
                  url={chat.avatar_url}
                />
                <span className="chat-row__content">
                  <span className="chat-row__line">
                    <strong>{chat.display_name}</strong>
                    <time>
                      {timeLabel(
                        chat.last_message?.occurred_at || chat.updated_at,
                      )}
                    </time>
                  </span>
                  <span className="chat-row__preview">
                    {preview(chat.last_message)}
                  </span>
                </span>
              </button>
            ))
          )}
          {conversations.hasNextPage && (
            <Button
              className="load-more"
              type="button"
              disabled={conversations.isFetchingNextPage}
              onClick={() => conversations.fetchNextPage()}
            >
              {conversations.isFetchingNextPage
                ? t(($) => $.chats.loadingMore)
                : t(($) => $.chats.loadOlder)}
            </Button>
          )}
        </div>
      ) : (
        <div className="sidebar-list">
          <button
            type="button"
            className="new-number"
            disabled={account.state !== "connected"}
            onClick={() => setShowPhone((value) => !value)}
          >
            <span className="new-number__icon">
              <HugeiconsIcon icon={SmartphoneIcon} size={20} />
            </span>
            <span>{t(($) => $.chats.phoneAction)}</span>
          </button>
          {showPhone && (
            <form className="new-number-form" onSubmit={startPhone}>
              <TextField
                label={t(($) => $.chats.phone)}
                hint={t(($) => $.chats.countryCode)}
                id="phone-number"
                type="tel"
                value={phone}
                onChange={(event) => setPhone(event.target.value)}
                placeholder={t(($) => $.chats.phoneExample)}
              />
              <Button
                type="submit"
                variant="primary"
                disabled={!phone.trim() || create.isPending}
              >
                {t(($) => $.chats.start)}
              </Button>
            </form>
          )}
          {account.state !== "connected" ? (
            <EmptyState
              title={t(($) => $.chats.connect)}
              description={t(($) => $.chats.connectDescription)}
            >
              <Button onClick={onPair}>{t(($) => $.chats.pair)}</Button>
            </EmptyState>
          ) : contacts.isPending ? (
            <Loading label={t(($) => $.chats.loadingContacts)} />
          ) : contacts.isError ? (
            <EmptyState title={t(($) => $.chats.contactsError)}>
              <Button variant="text" onClick={() => contacts.refetch()}>
                {t(($) => $.common.retry)}
              </Button>
            </EmptyState>
          ) : contacts.data?.pages.flatMap((page) => page.items).length ===
            0 ? (
            <EmptyState
              title={t(($) => $.chats.noContacts)}
              description={
                search
                  ? t(($) => $.chats.tryContactSearch)
                  : t(($) => $.chats.synced)
              }
            />
          ) : (
            contacts.data?.pages
              .flatMap((page) => page.items)
              .map((contact) => (
                <button
                  key={contact.provider_id}
                  type="button"
                  className="contact-row"
                  disabled={create.isPending}
                  onClick={() =>
                    create.mutate({
                      type: "contact",
                      value: contact.provider_id,
                    })
                  }
                >
                  <LiveAvatar
                    accountID={account.id}
                    name={contact.name}
                    url={contact.avatar_url}
                  />
                  <span>
                    <strong>{contact.name}</strong>
                    <small>
                      {contact.phone ||
                        contact.masked_phone ||
                        t(($) => $.chats.whatsappContact)}
                    </small>
                  </span>
                </button>
              ))
          )}
          {contacts.hasNextPage && (
            <Button
              className="load-more"
              type="button"
              disabled={contacts.isFetchingNextPage}
              onClick={() => contacts.fetchNextPage()}
            >
              {contacts.isFetchingNextPage
                ? t(($) => $.chats.loadingMore)
                : t(($) => $.chats.moreContacts)}
            </Button>
          )}
          {create.isError && (
            <p className="form-error sidebar-action-error" role="alert">
              {t(($) => $.errors[errorKey(create.error)])}
            </p>
          )}
        </div>
      )}
      <footer className="sidebar-account">
        <LiveAvatar
          accountID={account.id}
          name={account.label}
          url={account.provider_identity ? account.avatar_url : undefined}
          size="small"
        />
        <div className="sidebar-account__identity">
          <strong>{account.label}</strong>
          <ProviderBadge provider={account.provider} />
        </div>
        <div className="sidebar-account__connection">
          <StatusBadge state={account.state} />
          {account.state !== "connected" && (
            <Button variant="text" onClick={onPair}>
              {t(($) => $.chats.pair)}
            </Button>
          )}
        </div>
      </footer>
      {account.last_error && account.state !== "connected" && (
        <p className="sidebar-error">{t(($) => $.errors.connection)}</p>
      )}
    </aside>
  );
}
