import { Notification03Icon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { createContext, useContext, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button, IconButton } from "../../ui/Button";
import { Dialog } from "../../ui/Dialog";
import type { BrowserNotifications } from "./useBrowserNotifications";
import "./notifications.css";

export const NotificationContext = createContext<BrowserNotifications | null>(
  null,
);

export function NotificationSettings() {
  const notifications = useContext(NotificationContext);
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  if (!notifications) return null;
  const { status, failure } = notifications;

  async function disable() {
    if (!notifications) return;
    setBusy(true);
    setFailed(false);
    try {
      await notifications.disable();
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <IconButton
        className="rail-action"
        label={t(($) => $.notifications.title)}
        onClick={() => setOpen(true)}
      >
        <HugeiconsIcon icon={Notification03Icon} size={22} />
      </IconButton>
      {open && (
        <Dialog
          title={t(($) => $.notifications.title)}
          onClose={() => setOpen(false)}
        >
          <div className="notification-settings">
            <p role="status">
              {failure
                ? t(($) => $.notifications.failure[failure.stage])
                : t(($) => $.notifications.status[status])}
            </p>
            {failure?.detail && (
              <p className="form-error" role="alert">
                {failure.detail}
              </p>
            )}
            <label className="notification-preference">
              <input
                type="checkbox"
                checked={notifications.preview}
                onChange={(event) =>
                  notifications.setPreview(event.target.checked)
                }
              />
              <span>{t(($) => $.notifications.preview)}</span>
            </label>
            {failed && (
              <p className="form-error" role="alert">
                {t(($) => $.notifications.changeError)}
              </p>
            )}
            <div className="dialog__actions">
              {status === "enabled" ? (
                <Button busy={busy} onClick={disable}>
                  {t(($) => $.notifications.disable)}
                </Button>
              ) : (
                ["dismissed", "disabled", "error"].includes(status) && (
                  <Button
                    variant="primary"
                    disabled={busy}
                    onClick={notifications.enable}
                  >
                    {t(($) => $.notifications.enable)}
                  </Button>
                )
              )}
            </div>
          </div>
        </Dialog>
      )}
    </>
  );
}
