import { useEffect, useState } from "react";
import type { Message } from "../../api/types";

export function useMessageActions(message: Message) {
  const [now, setNow] = useState(Date.now);
  const actions = message.actions;
  const editUntil = actions?.edit_until
    ? Date.parse(actions.edit_until)
    : Infinity;
  const revokeUntil = actions?.revoke_until
    ? Date.parse(actions.revoke_until)
    : Infinity;
  useEffect(() => {
    const next = Math.min(
      ...[editUntil, revokeUntil].filter((value) => value > Date.now()),
    );
    if (!Number.isFinite(next)) return;
    const timer = window.setTimeout(
      () => setNow(Date.now()),
      Math.max(0, next - Date.now()) + 1,
    );
    return () => window.clearTimeout(timer);
  }, [editUntil, revokeUntil, now]);
  return {
    reply: !!actions?.reply && !message.deleted_at,
    react: !!actions?.react && !message.deleted_at,
    edit: !!actions?.edit && !message.deleted_at && now < editUntil,
    revoke: !!actions?.revoke && !message.deleted_at && now < revokeUntil,
    receipts: !!actions?.receipts && !message.deleted_at,
  };
}
