import { ArrowRight01Icon, LockPasswordIcon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { type FormEvent, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { type ErrorKey, errorKey } from "../../i18n/errors";
import { BrandMark } from "../../ui/BrandMark";
import { Button } from "../../ui/Button";
import { TextField } from "../../ui/Field";
import { LanguageSwitcher } from "../../ui/LanguageSwitcher";

export function Login({ onLogin }: { onLogin: () => void }) {
  const { t } = useTranslation();

  const [token, setToken] = useState("");
  const [error, setError] = useState<ErrorKey>();
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      await api.login(token.trim());
      setToken("");
      onLogin();
    } catch (cause) {
      setError(
        errorKey(cause) === "unauthorized" ? "invalidKey" : errorKey(cause),
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="login-page">
      <div className="login-language">
        <LanguageSwitcher />
      </div>
      <div className="login-brand">
        <span className="brand-symbol">
          <BrandMark decorative size={44} />
        </span>
        <span>ConvoMeow</span>
      </div>
      <div className="login-panel">
        <div className="login-panel__icon">
          <HugeiconsIcon icon={LockPasswordIcon} size={24} />
        </div>
        <h1>{t(($) => $.auth.title)}</h1>
        <p>
          <Trans
            i18nKey={($) => $.auth.instructions}
            components={{ code: <code /> }}
          />
        </p>
        <form onSubmit={submit}>
          <TextField
            label={t(($) => $.auth.key)}
            id="access-key"
            type="password"
            autoComplete="off"
            value={token}
            onChange={(event) => setToken(event.target.value)}
            placeholder={t(($) => $.auth.placeholder)}
            required
          />
          {error && (
            <p className="form-error" role="alert">
              {t(($) => $.errors[error])}
            </p>
          )}
          <Button
            variant="primary"
            type="submit"
            busy={busy}
            disabled={!token.trim()}
          >
            {busy ? t(($) => $.auth.busy) : t(($) => $.auth.open)}
            <HugeiconsIcon icon={ArrowRight01Icon} size={18} />
          </Button>
        </form>
      </div>
      <p className="login-foot">{t(($) => $.auth.server)}</p>
    </main>
  );
}
