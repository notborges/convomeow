import { type ReactNode, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";
import { type ActiveParticipant, presenceStore } from "../../api/presence";
import "./presence.css";

export function ChatPresenceIndicator({
  conversationID,
  group,
  fallback = null,
  announce = true,
}: {
  fallback?: ReactNode;
  announce?: boolean;
  conversationID: string;
  group: boolean;
}) {
  const people = useSyncExternalStore(presenceStore.subscribe, () =>
    presenceStore.get(conversationID),
  );
  return people.length ? (
    <PresenceIndicator people={people} group={group} announce={announce} />
  ) : (
    fallback
  );
}

export function PresenceIndicator({
  people,
  group,
  announce = true,
}: {
  announce?: boolean;
  people: readonly ActiveParticipant[];
  group: boolean;
}) {
  const { t, i18n } = useTranslation();
  if (!people.length) return null;
  const activity = (person: ActiveParticipant) =>
    person.activity === "recording"
      ? t(($) => $.presence.recording)
      : t(($) => $.presence.typing);
  const labels = people
    .slice(0, 2)
    .map((person) =>
      person.activity === "recording"
        ? t(($) => $.presence.personRecording, { name: person.name })
        : t(($) => $.presence.personTyping, { name: person.name }),
    );
  if (people.length > 2)
    labels.push(t(($) => $.presence.others, { count: people.length - 2 }));
  const label = group
    ? new Intl.ListFormat(i18n.resolvedLanguage, {
        style: "short",
        type: "conjunction",
      }).format(labels)
    : activity(people[0]);
  return (
    <span
      className="presence-indicator"
      role={announce ? "status" : undefined}
      title={label}
    >
      <span
        className={`presence-indicator__dots ${people.some((p) => p.activity === "recording") ? "presence-indicator__dots--recording" : ""}`}
        aria-hidden="true"
      >
        <i />
        <i />
        <i />
      </span>
      <span className="presence-indicator__label">{label}</span>
    </span>
  );
}
