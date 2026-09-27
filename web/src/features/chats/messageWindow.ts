import type { Message, Page } from "../../api/types";

export const initialMessageIndex = 1_000_000_000;

export function chronologicalMessages(pages: Page<Message>[]): Message[] {
  const seen = new Set<string>();
  return pages
    .flatMap((page) => page.items)
    .filter((message) => {
      if (seen.has(message.id)) return false;
      seen.add(message.id);
      return true;
    })
    .reverse();
}

// Keep overlapping messages at the same virtual index when pages enter or leave the window.
export function shiftedMessageIndex(
  previous: Message[],
  next: Message[],
  first: number,
): number {
  const positions = new Map(
    previous.map((message, index) => [message.id, index]),
  );
  for (let index = 0; index < next.length; index++) {
    const previousIndex = positions.get(next[index].id);
    if (previousIndex !== undefined) return first + previousIndex - index;
  }
  return initialMessageIndex;
}

export function messageGrouping(message: Message, previous?: Message) {
  const day = new Date(message.occurred_at).toDateString();
  const showDay =
    !previous || day !== new Date(previous.occurred_at).toDateString();
  return {
    showDay,
    grouped:
      !showDay &&
      !!previous &&
      previous.direction === message.direction &&
      previous.sender_id === message.sender_id &&
      Date.parse(message.occurred_at) - Date.parse(previous.occurred_at) <
        5 * 60 * 1000,
  };
}
