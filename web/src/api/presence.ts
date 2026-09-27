export type ChatActivity = "typing" | "recording" | "paused";
export type PresenceEvent = {
  type: "presence.changed";
  account_id: string;
  conversation_id: string;
  presence: {
    participant_id: string;
    display_name?: string;
    activity: ChatActivity;
    ttl_ms: number;
  };
};

export type ActiveParticipant = {
  id: string;
  name: string;
  activity: "typing" | "recording";
  expiresAt: number;
};
const empty: readonly ActiveParticipant[] = [];

export function createPresenceStore() {
  const entries = new Map<
    string,
    { accountID: string; people: readonly ActiveParticipant[] }
  >();
  const listeners = new Set<() => void>();
  let timer: ReturnType<typeof setTimeout> | undefined;
  function changed() {
    clearTimeout(timer);
    let next = Infinity;
    for (const { people } of entries.values())
      for (const person of people) next = Math.min(next, person.expiresAt);
    if (Number.isFinite(next))
      timer = setTimeout(expire, Math.max(0, next - Date.now()));
    for (const listener of listeners) listener();
  }
  function expire() {
    const now = Date.now();
    for (const [id, entry] of entries) {
      const people = entry.people.filter((person) => person.expiresAt > now);
      if (!people.length) entries.delete(id);
      else if (people.length !== entry.people.length)
        entries.set(id, { ...entry, people });
    }
    changed();
  }
  return {
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    get(conversationID: string) {
      return entries.get(conversationID)?.people ?? empty;
    },
    receive(event: PresenceEvent) {
      const { presence, conversation_id: id, account_id: accountID } = event;
      const people = (entries.get(id)?.people ?? empty).filter(
        (p) => p.id !== presence.participant_id && p.expiresAt > Date.now(),
      );
      if (presence.activity !== "paused" && presence.ttl_ms > 0) {
        people.push({
          id: presence.participant_id,
          name: presence.display_name || presence.participant_id.split("@")[0],
          activity: presence.activity,
          expiresAt: Date.now() + Math.min(presence.ttl_ms, 10000),
        });
      }
      if (people.length) {
        if (!entries.has(id) && entries.size >= 256)
          entries.delete(entries.keys().next().value as string);
        entries.set(id, { accountID, people: people.slice(-32) });
      } else entries.delete(id);
      changed();
    },
    clear(accountID?: string) {
      for (const [id, entry] of entries)
        if (!accountID || entry.accountID === accountID) entries.delete(id);
      changed();
    },
  };
}
export const presenceStore = createPresenceStore();
