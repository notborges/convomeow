import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { keys } from "../../api/queries";
import type { Account, Group } from "../../api/types";
import { errorKey } from "../../i18n/errors";
import { Button } from "../../ui/Button";
import { Dialog } from "../../ui/Dialog";
import { TextField } from "../../ui/Field";
import { ParticipantPicker, type Selection } from "./ParticipantPicker";
import { ParticipantResults } from "./ParticipantResults";

const maxNameLength = 100;

export function CreateGroupDialog({
  account,
  onCreated,
  onClose,
}: {
  account: Account;
  onCreated: (conversationID: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const [name, setName] = useState("");
  const [selected, setSelected] = useState<Selection>(new Map());
  const [created, setCreated] = useState<Group>();
  const create = useMutation({
    retry: false,
    mutationFn: () =>
      api.createGroup(account.id, name.trim(), [...selected.keys()]),
    onSuccess: (group) => {
      void client.invalidateQueries({
        queryKey: keys.conversations(account.id),
      });
      if (group.failed_participants?.length) setCreated(group);
      else onCreated(group.conversation_id);
    },
  });

  if (created)
    return (
      <Dialog title={t(($) => $.groups.created)} onClose={onClose}>
        <p className="group-note">{t(($) => $.groups.createdPartial)}</p>
        <ParticipantResults results={created.failed_participants ?? []} />
        <div className="dialog__actions">
          <Button
            variant="primary"
            onClick={() => onCreated(created.conversation_id)}
          >
            {t(($) => $.groups.open)}
          </Button>
        </div>
      </Dialog>
    );

  return (
    <Dialog
      title={t(($) => $.groups.create)}
      description={t(($) => $.groups.createDescription)}
      onClose={onClose}
      className="group-dialog"
    >
      <div className="group-form">
        <TextField
          label={t(($) => $.groups.name)}
          value={name}
          maxLength={maxNameLength}
          disabled={create.isPending}
          onChange={(event) => setName(event.target.value)}
        />
        <ParticipantPicker
          account={account}
          selected={selected}
          onChange={setSelected}
        />
        {create.isError && (
          <p className="form-error" role="alert">
            {t(($) => $.errors[errorKey(create.error)])}
          </p>
        )}
        <div className="dialog__actions">
          <Button onClick={onClose}>{t(($) => $.common.cancel)}</Button>
          <Button
            variant="primary"
            busy={create.isPending}
            disabled={!name.trim()}
            onClick={() => create.mutate()}
          >
            {t(($) => $.groups.createAction)}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
