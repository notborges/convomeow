export function armNotificationPrompt(
  target: EventTarget,
  eligible: () => boolean,
  subscribe: () => void,
) {
  function interaction(event: Event) {
    if (!event.isTrusted || !eligible()) return;
    if (event.type === "keydown") {
      const key = event as KeyboardEvent;
      if (
        key.isComposing ||
        key.ctrlKey ||
        key.metaKey ||
        key.altKey ||
        ["Escape", "Tab", "Shift", "Control", "Alt", "Meta"].includes(key.key)
      )
        return;
    }
    close();
    subscribe();
  }
  function close() {
    target.removeEventListener("click", interaction, true);
    target.removeEventListener("keydown", interaction, true);
  }
  target.addEventListener("click", interaction, true);
  target.addEventListener("keydown", interaction, true);
  return close;
}

export function applicationServerKey(value: string): ArrayBuffer {
  const raw = atob(value.replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from(raw, (character) => character.charCodeAt(0)).buffer;
}
