import {
  Copy01Icon,
  Edit02Icon,
  Link01Icon,
  Logout01Icon,
  UserAdd01Icon,
  UserMinus01Icon,
  UserRemove01Icon,
  UserStar01Icon,
} from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../../api/client";
import { keys } from "../../api/queries";
import type {
  Account,
  Conversation,
  Group,
  GroupParticipant,
  GroupParticipantAction,
  GroupParticipantResult,
  GroupUpdate,
} from "../../api/types";
import { LiveAvatar } from "../../app/LiveAvatar";
import { errorKey } from "../../i18n/errors";
import { ActionMenu } from "../../ui/ActionMenu";
import { ActionRow } from "../../ui/ActionRow";
import { Button, IconButton } from "../../ui/Button";
import { EmptyState } from "../../ui/EmptyState";
import { TextField } from "../../ui/Field";
import { Loading } from "../../ui/Loading";
import { Switch } from "../../ui/Switch";
import { ParticipantPicker, type Selection } from "./ParticipantPicker";
import { ParticipantResults } from "./ParticipantResults";

type View =
  | { kind: "overview" }
  | { kind: "edit" }
  | { kind: "add" }
  | { kind: "leave" }
  | { kind: "resetLink" }
  | { kind: "remove"; participant: GroupParticipant };

export function GroupDetails({
  account,
  conversation,
}: {
  account: Account;
  conversation: Conversation;
}) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const [view, setView] = useState<View>({ kind: "overview" });
  const [results, setResults] = useState<GroupParticipantResult[]>([]);
  const connected = account.state === "connected";
  const group = useQuery({
    queryKey: keys.group(conversation.id),
    meta: { accountID: account.id },
    queryFn: () => api.group(conversation.id),
    enabled: connected,
    retry: (count, error) =>
      count < 2 && !(error instanceof ApiError && error.status < 500),
  });

  function refresh(data?: Group) {
    if (data) client.setQueryData(keys.group(conversation.id), data);
    else
      void client.invalidateQueries({ queryKey: keys.group(conversation.id) });
    void client.invalidateQueries({
      queryKey: keys.conversation(conversation.id),
    });
    void client.invalidateQueries({ queryKey: keys.conversations(account.id) });
  }

  const update = useMutation({
    retry: false,
    mutationFn: (change: GroupUpdate) =>
      api.updateGroup(conversation.id, change),
    onSuccess: (data) => {
      refresh(data);
      setView({ kind: "overview" });
    },
  });
  const participants = useMutation({
    retry: false,
    mutationFn: ({
      action,
      ids,
    }: {
      action: GroupParticipantAction;
      ids: string[];
    }) => api.changeGroupParticipants(conversation.id, action, ids),
    onSuccess: (page) => {
      setResults(page.items.filter((item) => item.status !== "ok"));
      refresh();
      setView({ kind: "overview" });
    },
  });
  const leave = useMutation({
    retry: false,
    mutationFn: () => api.leaveGroup(conversation.id),
    onSuccess: () => {
      refresh();
      setView({ kind: "overview" });
    },
  });

  const header = (data?: Group, canEdit = false) => (
    <header className="group-header">
      <LiveAvatar
        accountID={account.id}
        name={data?.name ?? conversation.display_name}
        url={conversation.avatar_url}
        size="large"
      />
      <div className="group-header__title">
        <h3>{data?.name ?? conversation.display_name}</h3>
        {canEdit && (
          <IconButton
            label={t(($) => $.groups.edit)}
            onClick={() => setView({ kind: "edit" })}
          >
            <HugeiconsIcon icon={Edit02Icon} size={18} />
          </IconButton>
        )}
      </div>
      <p>
        {data
          ? t(($) => $.groups.summary, { count: data.participants.length })
          : t(($) => $.thread.group)}
      </p>
      {(data?.description ?? conversation.description) && (
        <p className="group-header__description">
          {data?.description ?? conversation.description}
        </p>
      )}
    </header>
  );

  if (!connected || group.isPending || group.isError)
    return (
      <div className="group-details">
        {header()}
        {!connected ? (
          <p className="group-note">{t(($) => $.groups.connect)}</p>
        ) : group.isPending ? (
          <Loading label={t(($) => $.groups.loading)} />
        ) : group.error instanceof ApiError &&
          group.error.code === "not_group_participant" ? (
          <p className="group-note">{t(($) => $.groups.notParticipant)}</p>
        ) : (
          <EmptyState title={t(($) => $.groups.loadError)}>
            <Button variant="text" onClick={() => group.refetch()}>
              {t(($) => $.common.retry)}
            </Button>
          </EmptyState>
        )}
      </div>
    );

  const data = group.data;
  const admin = data.own_role !== "member";
  const canEdit = admin || !data.admins_only_edit;
  const error = update.error ?? participants.error ?? leave.error;
  const busy = update.isPending || participants.isPending || leave.isPending;
  const back = () => setView({ kind: "overview" });

  if (view.kind === "edit")
    return (
      <EditGroup
        group={data}
        busy={update.isPending}
        error={update.error}
        onCancel={back}
        onSave={(change) => update.mutate(change)}
      />
    );
  if (view.kind === "add")
    return (
      <AddParticipants
        account={account}
        group={data}
        busy={participants.isPending}
        error={participants.error}
        onCancel={back}
        onAdd={(ids) => participants.mutate({ action: "add", ids })}
      />
    );
  if (view.kind === "leave" || view.kind === "remove")
    return (
      <Confirm
        message={
          view.kind === "leave"
            ? t(($) => $.groups.leaveConfirm, { name: data.name })
            : t(($) => $.groups.removeConfirm, {
                name: participantName(view.participant),
              })
        }
        action={
          view.kind === "leave"
            ? t(($) => $.groups.leave)
            : t(($) => $.groups.remove)
        }
        busy={busy}
        error={error}
        onCancel={back}
        onConfirm={() =>
          view.kind === "leave"
            ? leave.mutate()
            : participants.mutate({
                action: "remove",
                ids: [view.participant.provider_id],
              })
        }
      />
    );

  function participantName(participant: GroupParticipant) {
    if (participant.is_self) return t(($) => $.reply.you);
    return (
      participant.display_name ||
      participant.phone ||
      participant.provider_id.split("@")[0]
    );
  }

  function participantActions(participant: GroupParticipant) {
    if (!admin || participant.is_self || participant.role === "owner")
      return [];
    const change = (action: GroupParticipantAction) => () =>
      participants.mutate({ action, ids: [participant.provider_id] });
    return [
      participant.role === "member"
        ? {
            id: "promote",
            label: t(($) => $.groups.promote),
            icon: <HugeiconsIcon icon={UserStar01Icon} size={18} />,
            onSelect: change("promote"),
          }
        : {
            id: "demote",
            label: t(($) => $.groups.demote),
            icon: <HugeiconsIcon icon={UserMinus01Icon} size={18} />,
            onSelect: change("demote"),
          },
      {
        id: "remove",
        label: t(($) => $.groups.remove),
        icon: <HugeiconsIcon icon={UserRemove01Icon} size={18} />,
        movesFocus: true,
        onSelect: () => setView({ kind: "remove", participant }),
      },
    ];
  }

  const avatarURL = (participant: GroupParticipant) => {
    if (!account.capabilities?.includes("read_avatars")) return undefined;
    if (participant.is_self) return account.avatar_url;
    return `/api/v1/accounts/${encodeURIComponent(account.id)}/contacts/${encodeURIComponent(participant.provider_id)}/avatar`;
  };

  return (
    <div className="group-details">
      {header(data, canEdit && !busy)}
      {error && (
        <p className="form-error" role="alert">
          {t(($) => $.errors[errorKey(error)])}
        </p>
      )}
      <section className="group-section">
        <h4>{t(($) => $.groups.participantsTitle)}</h4>
        {admin && (
          <ActionRow
            icon={<HugeiconsIcon icon={UserAdd01Icon} size={20} />}
            disabled={busy}
            onClick={() => setView({ kind: "add" })}
          >
            {t(($) => $.groups.add)}
          </ActionRow>
        )}
        {admin && (
          <InviteLink
            conversationID={conversation.id}
            confirmReset={view.kind === "resetLink"}
            onReset={() => setView({ kind: "resetLink" })}
            onCancelReset={back}
          />
        )}
        <ParticipantResults results={results} />
        <ul className="group-participants">
          {data.participants.map((participant) => (
            <li key={participant.provider_id}>
              <LiveAvatar
                accountID={account.id}
                name={participantName(participant)}
                url={avatarURL(participant)}
                size="small"
              />
              <span className="group-participants__name">
                <strong>{participantName(participant)}</strong>
                {participant.phone && participant.display_name && (
                  <small>{participant.phone}</small>
                )}
              </span>
              {participant.role !== "member" && (
                <span className="group-role">
                  {participant.role === "owner"
                    ? t(($) => $.groups.owner)
                    : t(($) => $.groups.admin)}
                </span>
              )}
              <ActionMenu
                label={t(($) => $.groups.participantActions, {
                  name: participantName(participant),
                })}
                actions={participantActions(participant)}
              />
            </li>
          ))}
        </ul>
      </section>
      {admin && (
        <section className="group-section">
          <h4>{t(($) => $.groups.settings)}</h4>
          <Switch
            label={t(($) => $.groups.adminsOnlySend)}
            checked={data.admins_only_send}
            disabled={busy}
            onChange={(checked) => update.mutate({ admins_only_send: checked })}
          />
          <Switch
            label={t(($) => $.groups.adminsOnlyEdit)}
            checked={data.admins_only_edit}
            disabled={busy}
            onChange={(checked) => update.mutate({ admins_only_edit: checked })}
          />
        </section>
      )}
      <section className="group-section">
        <ActionRow
          tone="danger"
          icon={<HugeiconsIcon icon={Logout01Icon} size={20} />}
          disabled={busy}
          onClick={() => setView({ kind: "leave" })}
        >
          {t(($) => $.groups.leave)}
        </ActionRow>
      </section>
    </div>
  );
}

function EditGroup({
  group,
  busy,
  error,
  onCancel,
  onSave,
}: {
  group: Group;
  busy: boolean;
  error: Error | null;
  onCancel: () => void;
  onSave: (change: GroupUpdate) => void;
}) {
  const { t } = useTranslation();
  const id = useId();
  const [name, setName] = useState(group.name);
  const [description, setDescription] = useState(group.description ?? "");
  const change: GroupUpdate = {};
  if (name.trim() !== group.name) change.name = name.trim();
  if (description.trim() !== (group.description ?? ""))
    change.description = description.trim();
  return (
    <form
      className="group-form"
      onSubmit={(event) => {
        event.preventDefault();
        if (name.trim() && Object.keys(change).length > 0) onSave(change);
      }}
    >
      <TextField
        label={t(($) => $.groups.name)}
        value={name}
        maxLength={100}
        disabled={busy}
        onChange={(event) => setName(event.target.value)}
      />
      <div className="field">
        <label htmlFor={id}>{t(($) => $.groups.description)}</label>
        <textarea
          id={id}
          className="input group-form__description"
          value={description}
          maxLength={2048}
          disabled={busy}
          onChange={(event) => setDescription(event.target.value)}
        />
      </div>
      {error && (
        <p className="form-error" role="alert">
          {t(($) => $.errors[errorKey(error)])}
        </p>
      )}
      <div className="dialog__actions">
        <Button onClick={onCancel}>{t(($) => $.common.cancel)}</Button>
        <Button
          type="submit"
          variant="primary"
          busy={busy}
          disabled={!name.trim() || Object.keys(change).length === 0}
        >
          {t(($) => $.groups.save)}
        </Button>
      </div>
    </form>
  );
}

function AddParticipants({
  account,
  group,
  busy,
  error,
  onCancel,
  onAdd,
}: {
  account: Account;
  group: Group;
  busy: boolean;
  error: Error | null;
  onCancel: () => void;
  onAdd: (ids: string[]) => void;
}) {
  const { t } = useTranslation();
  const [selected, setSelected] = useState<Selection>(new Map());
  return (
    <div className="group-form">
      <ParticipantPicker
        account={account}
        selected={selected}
        exclude={new Set(group.participants.map((p) => p.provider_id))}
        onChange={setSelected}
      />
      {error && (
        <p className="form-error" role="alert">
          {t(($) => $.errors[errorKey(error)])}
        </p>
      )}
      <div className="dialog__actions">
        <Button onClick={onCancel}>{t(($) => $.common.cancel)}</Button>
        <Button
          variant="primary"
          busy={busy}
          disabled={selected.size === 0}
          onClick={() => onAdd([...selected.keys()])}
        >
          {t(($) => $.groups.addAction)}
        </Button>
      </div>
    </div>
  );
}

function Confirm({
  message,
  action,
  busy,
  error,
  onCancel,
  onConfirm,
}: {
  message: string;
  action: string;
  busy: boolean;
  error: Error | null;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="group-form">
      <p>{message}</p>
      {error && (
        <p className="form-error" role="alert">
          {t(($) => $.errors[errorKey(error)])}
        </p>
      )}
      <div className="dialog__actions">
        <Button onClick={onCancel}>{t(($) => $.common.cancel)}</Button>
        <Button
          variant="primary"
          className="group-danger"
          busy={busy}
          onClick={onConfirm}
        >
          {action}
        </Button>
      </div>
    </div>
  );
}

function InviteLink({
  conversationID,
  confirmReset,
  onReset,
  onCancelReset,
}: {
  conversationID: string;
  confirmReset: boolean;
  onReset: () => void;
  onCancelReset: () => void;
}) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);
  const link = useMutation({
    retry: false,
    mutationFn: (reset: boolean) => api.groupInviteLink(conversationID, reset),
    onSuccess: () => {
      setCopied(false);
      onCancelReset();
    },
  });
  return (
    <>
      <ActionRow
        icon={<HugeiconsIcon icon={Link01Icon} size={20} />}
        disabled={link.isPending}
        aria-expanded={!!link.data}
        onClick={() => link.mutate(false)}
      >
        {t(($) => $.groups.inviteLink)}
      </ActionRow>
      {link.data && (
        <div className="group-invite">
          <code>{link.data.url}</code>
          {confirmReset ? (
            <>
              <p>{t(($) => $.groups.resetConfirm)}</p>
              <div className="group-invite__actions">
                <Button variant="text" onClick={onCancelReset}>
                  {t(($) => $.common.cancel)}
                </Button>
                <Button
                  variant="primary"
                  className="group-danger"
                  busy={link.isPending}
                  onClick={() => link.mutate(true)}
                >
                  {t(($) => $.groups.resetLink)}
                </Button>
              </div>
            </>
          ) : (
            <div className="group-invite__actions">
              <Button variant="text" onClick={onReset}>
                {t(($) => $.groups.resetLink)}
              </Button>
              <Button
                onClick={() =>
                  navigator.clipboard
                    .writeText(link.data.url)
                    .then(() => setCopied(true))
                    .catch(() => setCopied(false))
                }
              >
                <HugeiconsIcon icon={Copy01Icon} size={18} />
                {copied ? t(($) => $.message.copied) : t(($) => $.message.copy)}
              </Button>
            </div>
          )}
        </div>
      )}
      {link.isError && (
        <p className="form-error" role="alert">
          {t(($) => $.errors[errorKey(link.error)])}
        </p>
      )}
    </>
  );
}
