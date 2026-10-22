import { useTranslation } from "react-i18next";
import type { Account, Conversation } from "../../api/types";
import { Avatar } from "../../ui/Avatar";
import { Dialog } from "../../ui/Dialog";
import { ProviderBadge } from "../../ui/ProviderBadge";
import { StatusBadge } from "../../ui/StatusBadge";
import { GroupDetails } from "../groups/GroupDetails";

export function ConversationDetails({
  conversation,
  account,
  onClose,
}: {
  conversation: Conversation;
  account: Account;
  onClose: () => void;
}) {
  const { t } = useTranslation();

  const phone =
    conversation.contact?.phone || conversation.contact?.masked_phone;
  return (
    <Dialog
      title={
        conversation.kind === "group"
          ? t(($) => $.details.group)
          : t(($) => $.details.contact)
      }
      onClose={onClose}
    >
      {conversation.kind === "group" &&
      account.capabilities?.includes("manage_groups") ? (
        <GroupDetails account={account} conversation={conversation} />
      ) : (
        <div className="contact-details">
          <Avatar
            name={conversation.display_name}
            url={conversation.avatar_url}
            size="large"
          />
          <h3>{conversation.display_name}</h3>
          {phone && <p>{phone}</p>}
          {conversation.description && (
            <p className="contact-details__description">
              {conversation.description}
            </p>
          )}
        </div>
      )}
      <dl className="details-list">
        {conversation.kind !== "group" && (
          <div>
            <dt>{t(($) => $.thread.conversation)}</dt>
            <dd>{t(($) => $.details.direct)}</dd>
          </div>
        )}
        <div>
          <dt>{t(($) => $.details.account)}</dt>
          <dd>{account.label}</dd>
        </div>
        <div>
          <dt>{t(($) => $.details.service)}</dt>
          <dd>
            <ProviderBadge provider={account.provider} />
          </dd>
        </div>
        <div>
          <dt>{t(($) => $.details.connection)}</dt>
          <dd>
            <StatusBadge state={account.state} />
          </dd>
        </div>
      </dl>
    </Dialog>
  );
}
