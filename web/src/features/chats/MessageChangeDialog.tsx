import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../../api/client";
import { keys } from "../../api/queries";
import type { Message } from "../../api/types";
import { errorKey } from "../../i18n/errors";
import { Button } from "../../ui/Button";
import { Dialog } from "../../ui/Dialog";
import { useMessageActions } from "./useMessageActions";

export function MessageChangeDialog({
  message,
  kind,
  onClose,
}: {
  message: Message;
  kind: "edit" | "revoke";
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const id = useId();
  const client = useQueryClient();
  const actions = useMessageActions(message);
  const [text, setText] = useState(message.content.text ?? "");
  const available = kind === "edit" ? actions.edit : actions.revoke;
  const mutation = useMutation({
    retry: false,
    mutationFn: () =>
      kind === "edit"
        ? api.editMessage(message.id, text.trim())
        : api.revokeMessage(message.id),
    onSuccess: onClose,
    onSettled: () => {
      void client.invalidateQueries({
        queryKey: keys.messages(message.conversation_id),
      });
      void client.invalidateQueries({
        queryKey: keys.conversations(message.account_id),
      });
    },
  });
  return (
    <Dialog
      title={t(($) =>
        kind === "edit" ? $.messageChanges.edit : $.messageChanges.revoke,
      )}
      onClose={onClose}
    >
      <form
        className="message-change-form"
        onSubmit={(event) => {
          event.preventDefault();
          if (available && !mutation.isPending && !mutation.isError)
            mutation.mutate();
        }}
      >
        {kind === "edit" ? (
          <div className="field">
            <label htmlFor={id}>{t(($) => $.composer.message)}</label>
            <textarea
              id={id}
              className="input message-change-form__text"
              value={text}
              disabled={mutation.isPending}
              maxLength={4096}
              onChange={(event) => setText(event.target.value)}
            />
          </div>
        ) : (
          <p>{t(($) => $.messageChanges.revokeDescription)}</p>
        )}
        {!available && (
          <p role="status">{t(($) => $.messageChanges.unavailable)}</p>
        )}
        {mutation.isError && (
          <p className="form-error" role="alert">
            {mutation.error instanceof ApiError &&
            mutation.error.code === "message_change_unconfirmed"
              ? t(($) => $.messageChanges.unconfirmed)
              : t(($) => $.errors[errorKey(mutation.error)])}
          </p>
        )}
        <div className="message-change-form__actions">
          <Button onClick={onClose}>{t(($) => $.common.cancel)}</Button>
          <Button
            type="submit"
            variant="primary"
            busy={mutation.isPending}
            disabled={
              !available ||
              mutation.isError ||
              (kind === "edit" &&
                (!text.trim() || text.trim() === message.content.text))
            }
          >
            {t(($) =>
              kind === "edit" ? $.messageChanges.save : $.messageChanges.revoke,
            )}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
