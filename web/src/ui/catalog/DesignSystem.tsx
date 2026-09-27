import {
  Add01Icon,
  Chat01Icon,
  SendHorizontalIcon,
} from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { Message } from "../../api/types";
import { MessageBubble } from "../../features/chats/MessageBubble";
import { PresenceIndicator } from "../../features/presence/PresenceIndicator";
import { AudioPlayer } from "../AudioPlayer";
import { Avatar } from "../Avatar";
import { BrandMark } from "../BrandMark";
import { Button, IconButton } from "../Button";
import { Dialog } from "../Dialog";
import { EmptyState } from "../EmptyState";
import { SearchField, TextField } from "../Field";
import { ImageViewer } from "../ImageViewer";
import { LanguageSwitcher } from "../LanguageSwitcher";
import { Loading } from "../Loading";
import { ProviderBadge } from "../ProviderBadge";
import { SegmentedControl } from "../SegmentedControl";
import { StatusBadge } from "../StatusBadge";
import { ThemeToggle } from "../ThemeToggle";
import "./catalog.css";

export default function DesignSystem() {
  const { t } = useTranslation();
  const sample: Message = {
    id: "preview",
    account_id: "preview",
    conversation_id: "preview",
    direction: "inbound",
    state: "sent",
    kind: "text",
    content: { text: t(($) => $.catalog.sampleHello) },
    occurred_at: "2026-01-01T10:00:00Z",
  };

  const [replySelected, setReplySelected] = useState(false);
  const [search, setSearch] = useState("");
  const [tab, setTab] = useState("chats");
  const [dialog, setDialog] = useState(false);
  const [viewer, setViewer] = useState(false);
  return (
    <main className="catalog">
      <header className="catalog__header">
        <div>
          <h1>{t(($) => $.catalog.title)}</h1>
          <p>{t(($) => $.catalog.description)}</p>
        </div>
        <div className="catalog__theme">
          <LanguageSwitcher rail />
          <ThemeToggle />
        </div>
      </header>
      <section>
        <h2>{t(($) => $.presence.typing)}</h2>
        <div className="catalog__row">
          <PresenceIndicator
            group={false}
            people={[
              {
                id: "maya",
                name: "Maya",
                activity: "typing",
                expiresAt: Infinity,
              },
            ]}
          />
          <PresenceIndicator
            group
            people={[
              {
                id: "maya",
                name: "Maya",
                activity: "recording",
                expiresAt: Infinity,
              },
            ]}
          />
        </div>
      </section>
      <section>
        <h2>{t(($) => $.media.audio)}</h2>
        <AudioPlayer />
        <AudioPlayer loading />
        <AudioPlayer unavailable />
      </section>
      <section>
        <h2>{t(($) => $.catalog.color)}</h2>
        <p>{t(($) => $.catalog.colorsDescription)}</p>
        <div className="catalog__swatches">
          {[
            "canvas",
            "surface",
            "surface-subtle",
            "surface-selected",
            "accent",
            "outgoing",
            "danger-subtle",
          ].map((token) => (
            <div key={token}>
              <span style={{ background: `var(--${token})` }} />
              <code>--{token}</code>
            </div>
          ))}
        </div>
      </section>
      <section>
        <h2>{t(($) => $.catalog.type)}</h2>
        <div className="catalog__type">
          <strong>{t(($) => $.chats.title)}</strong>
          <span>{t(($) => $.catalog.typeSample)}</span>
          <small>{t(($) => $.catalog.metadata)}</small>
        </div>
        <p>{t(($) => $.catalog.typeDescription)}</p>
      </section>
      <section>
        <h2>{t(($) => $.catalog.actions)}</h2>
        <div className="catalog__row">
          <Button variant="primary">{t(($) => $.catalog.start)}</Button>
          <Button>{t(($) => $.accounts.add)}</Button>
          <Button variant="ghost">{t(($) => $.common.cancel)}</Button>
          <Button variant="text">{t(($) => $.common.retry)}</Button>
          <IconButton variant="primary" label={t(($) => $.composer.send)}>
            <HugeiconsIcon icon={SendHorizontalIcon} size={22} />
          </IconButton>
          <IconButton label={t(($) => $.accounts.add)}>
            <HugeiconsIcon icon={Add01Icon} size={22} />
          </IconButton>
        </div>
        <div className="catalog__row">
          <Button variant="primary" disabled>
            {t(($) => $.catalog.unavailable)}
          </Button>
          <Button busy>{t(($) => $.catalog.connecting)}</Button>
          <Button onClick={() => setDialog(true)}>
            {t(($) => $.catalog.openDialog)}
          </Button>
        </div>
        <p>{t(($) => $.catalog.actionsDescription)}</p>
      </section>
      <section>
        <h2>{t(($) => $.catalog.inputs)}</h2>
        <div className="catalog__fields">
          <TextField
            label={t(($) => $.accounts.name)}
            placeholder={t(($) => $.catalog.personal)}
            hint={t(($) => $.catalog.nameHint)}
          />
          <TextField
            label={t(($) => $.auth.key)}
            type="password"
            defaultValue="example"
            error={t(($) => $.catalog.invalidKey)}
          />
          <SearchField
            label={t(($) => $.chats.searchContacts)}
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
            onChange={setTab}
          />
        </div>
      </section>
      <section>
        <h2>{t(($) => $.catalog.identity)}</h2>
        <div className="catalog__row">
          <BrandMark size={24} />
          <BrandMark size={40} />
          <BrandMark size={80} />
        </div>
        <div className="catalog__row">
          <ProviderBadge provider="whatsapp" />
          <ProviderBadge provider="telegram" />
          <ProviderBadge provider="other" />
          <ProviderBadge provider="whatsapp" compact />
        </div>
        <div className="catalog__row">
          {[
            "Maya Chen",
            "Oliver",
            "Family",
            "Weekend plans",
            "Alice",
            "Liam",
          ].map((name) => (
            <Avatar key={name} name={name} />
          ))}
          <StatusBadge state="connected" />
          <StatusBadge state="pairing" />
          <StatusBadge state="disconnected" />
        </div>
      </section>
      <section>
        <h2>{t(($) => $.chats.title)}</h2>
        {replySelected && <p role="status">{t(($) => $.reply.action)}</p>}
        <div className="catalog__messages">
          {(["unknown", "delivered", "read", "partial_read"] as const).map(
            (state) => (
              <MessageBubble
                key={state}
                message={{
                  ...sample,
                  id: `receipt-${state}`,
                  direction: "outbound",
                  delivery: {
                    state,
                    group: state === "partial_read",
                    delivered_count: state === "unknown" ? 0 : 2,
                    read_count:
                      state === "read" || state === "partial_read" ? 1 : 0,
                  },
                }}
                grouped={false}
                showSender={false}
              />
            ),
          )}

          <MessageBubble
            message={{
              ...sample,
              reply: { kind: "text", text: t(($) => $.catalog.sampleHello) },
            }}
            grouped={false}
            showSender={false}
            onReply={() => setReplySelected(true)}
          />
          <MessageBubble
            message={{
              ...sample,
              direction: "outbound",
              content: { text: t(($) => $.catalog.sampleReply) },
            }}
            grouped={false}
            showSender={false}
            onReply={() => setReplySelected(true)}
          />
          <MessageBubble
            message={{
              ...sample,
              direction: "outbound",
              content: { text: t(($) => $.catalog.sampleCoffee) },
            }}
            grouped
            showSender={false}
          />
        </div>
      </section>
      <section>
        <h2>{t(($) => $.catalog.empty)}</h2>
        <div className="catalog__fields">
          <EmptyState
            icon={<HugeiconsIcon icon={Chat01Icon} size={28} />}
            title={t(($) => $.chats.empty)}
            description={t(($) => $.chats.sayHello)}
          >
            <Button variant="text">{t(($) => $.chats.browseContacts)}</Button>
          </EmptyState>
          <Loading label={t(($) => $.catalog.loading)} />
        </div>
      </section>
      <Button onClick={() => setViewer(true)}>{t(($) => $.viewer.open)}</Button>
      {viewer && (
        <ImageViewer
          src="/app/brand/convomeow.png"
          name="ConvoMeow"
          onClose={() => setViewer(false)}
        />
      )}
      {dialog && (
        <Dialog
          title={t(($) => $.accounts.addTitle)}
          description={t(($) => $.catalog.dialogDescription)}
          onClose={() => setDialog(false)}
        >
          <TextField
            label={t(($) => $.accounts.name)}
            placeholder={t(($) => $.catalog.personal)}
          />
          <div className="dialog__actions">
            <Button variant="ghost" onClick={() => setDialog(false)}>
              {t(($) => $.common.cancel)}
            </Button>
            <Button variant="primary" onClick={() => setDialog(false)}>
              {t(($) => $.accounts.add)}
            </Button>
          </div>
        </Dialog>
      )}
    </main>
  );
}
