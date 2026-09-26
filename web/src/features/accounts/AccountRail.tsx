import { Add01Icon, Logout01Icon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { keys } from "../../api/queries";
import type { Account, Page } from "../../api/types";
import { LiveAvatar } from "../../app/LiveAvatar";
import { type ErrorKey, errorKey } from "../../i18n/errors";
import { connectionLabel } from "../../i18n/format";
import { BrandMark } from "../../ui/BrandMark";
import { Button, IconButton } from "../../ui/Button";
import { Dialog } from "../../ui/Dialog";
import { TextField } from "../../ui/Field";
import { LanguageSwitcher } from "../../ui/LanguageSwitcher";
import { ProviderBadge, providerName } from "../../ui/ProviderBadge";
import { ThemeToggle } from "../../ui/ThemeToggle";

interface Props {
  accounts: Account[];
  selectedID?: string;
  onSelect: (id: string) => void;
  onCreated: (id: string) => void;
  onLogout: () => Promise<void>;
}

export function AccountRail({
  accounts,
  selectedID,
  onSelect,
  onCreated,
  onLogout,
}: Props) {
  const { t } = useTranslation();

  const [adding, setAdding] = useState(false);
  const [label, setLabel] = useState("");
  const [logoutError, setLogoutError] = useState<ErrorKey>();
  const queryClient = useQueryClient();
  const create = useMutation({
    mutationFn: () => api.createAccount(label.trim()),
    onSuccess: (account) => {
      queryClient.setQueryData<Page<Account>>(keys.accounts, (current) => ({
        items: [...(current?.items ?? []), account],
      }));
      queryClient.invalidateQueries({ queryKey: keys.accounts });
      setLabel("");
      setAdding(false);
      onCreated(account.id);
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (label.trim()) create.mutate();
  }

  async function logout() {
    setLogoutError(undefined);
    try {
      await onLogout();
    } catch (cause) {
      setLogoutError(errorKey(cause));
    }
  }

  return (
    <aside className="account-rail" aria-label={t(($) => $.accounts.title)}>
      <div className="rail-brand" title="ConvoMeow">
        <BrandMark decorative size={44} />
      </div>
      <div className="rail-accounts">
        {accounts.map((account) => (
          <button
            key={account.id}
            className={`rail-account ${account.id === selectedID ? "rail-account--active" : ""}`}
            type="button"
            title={t(($) => $.accounts.open, {
              name: account.label,
              provider: providerName(account.provider),
              state: connectionLabel(account.state),
            })}
            aria-label={t(($) => $.accounts.open, {
              name: account.label,
              provider: providerName(account.provider),
              state: connectionLabel(account.state),
            })}
            aria-current={account.id === selectedID ? "page" : undefined}
            onClick={() => onSelect(account.id)}
          >
            <LiveAvatar
              accountID={account.id}
              name={account.label}
              url={account.provider_identity ? account.avatar_url : undefined}
              size="small"
            />
            <span className="rail-account__provider">
              <ProviderBadge provider={account.provider} compact />
            </span>
          </button>
        ))}
        <IconButton
          className="rail-action"
          label={t(($) => $.accounts.addWhatsApp)}
          onClick={() => setAdding(true)}
        >
          <HugeiconsIcon icon={Add01Icon} size={23} />
        </IconButton>
      </div>
      <div className="rail-footer">
        <LanguageSwitcher rail />
        <ThemeToggle />
        <IconButton
          className="rail-action"
          label={t(($) => $.accounts.signOut)}
          onClick={logout}
        >
          <HugeiconsIcon icon={Logout01Icon} size={22} />
        </IconButton>
      </div>
      {logoutError && (
        <Dialog
          title={t(($) => $.accounts.signOutError)}
          onClose={() => setLogoutError(undefined)}
        >
          <p className="form-error" role="alert">
            {t(($) => $.errors[logoutError])}
          </p>
        </Dialog>
      )}
      {adding && (
        <Dialog
          title={t(($) => $.accounts.addTitle)}
          description={t(($) => $.accounts.addDescription)}
          onClose={() => setAdding(false)}
        >
          <form className="account-form" onSubmit={submit}>
            <TextField
              label={t(($) => $.accounts.name)}
              id="account-name"
              value={label}
              onChange={(event) => setLabel(event.target.value)}
              placeholder={t(($) => $.accounts.example)}
              hint={t(($) => $.accounts.nameHint)}
              pattern={"[A-Za-z0-9_\\-]+"}
              required
              maxLength={64}
            />
            {create.isError && (
              <p className="form-error" role="alert">
                {t(($) => $.errors[errorKey(create.error)])}
              </p>
            )}
            <div className="dialog__actions">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setAdding(false)}
              >
                {t(($) => $.common.cancel)}
              </Button>
              <Button
                type="submit"
                variant="primary"
                disabled={!label.trim() || create.isPending}
              >
                {create.isPending
                  ? t(($) => $.accounts.adding)
                  : t(($) => $.accounts.add)}
              </Button>
            </div>
          </form>
        </Dialog>
      )}
    </aside>
  );
}
