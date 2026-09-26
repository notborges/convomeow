import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { QRCodeSVG } from "qrcode.react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { keys } from "../../api/queries";
import type { Account } from "../../api/types";
import { errorKey } from "../../i18n/errors";
import { Button } from "../../ui/Button";
import { Dialog } from "../../ui/Dialog";
import { Loading } from "../../ui/Loading";

export function PairingDialog({
  account,
  existingAttemptID,
  onStarted,
  onClose,
}: {
  account: Account;
  existingAttemptID?: string;
  onStarted: (id: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();

  const [attemptID, setAttemptID] = useState(existingAttemptID);
  const queryClient = useQueryClient();
  const start = useMutation({
    mutationFn: () => api.startLogin(account.id),
    onSuccess: (attempt) => {
      setAttemptID(attempt.id);
      onStarted(attempt.id);
    },
  });
  const attempt = useQuery({
    queryKey: ["login-attempt", account.id, attemptID],
    queryFn: () => api.loginAttempt(account.id, attemptID ?? ""),
    enabled: Boolean(attemptID),
  });

  useEffect(() => {
    if (!existingAttemptID) start.mutate();
  }, []);

  useEffect(() => {
    if (attempt.data?.state === "connected")
      queryClient.invalidateQueries({ queryKey: keys.accounts });
  }, [attempt.data?.state, queryClient]);

  return (
    <Dialog
      title={t(($) => $.pair.title, { name: account.label })}
      description={t(($) => $.pair.instructions)}
      onClose={onClose}
    >
      <div className="pair-dialog__body">
        {start.isError ? (
          <div className="pair-error">
            <p>{t(($) => $.errors[errorKey(start.error)])}</p>
            <Button type="button" onClick={() => start.mutate()}>
              {t(($) => $.common.retry)}
            </Button>
          </div>
        ) : attempt.isError ? (
          <div className="pair-error">
            <p>{t(($) => $.errors[errorKey(attempt.error)])}</p>
            <Button type="button" onClick={() => attempt.refetch()}>
              {t(($) => $.common.retry)}
            </Button>
          </div>
        ) : start.isPending || attempt.isPending ? (
          <Loading label={t(($) => $.pair.preparing)} />
        ) : attempt.data?.state === "connected" ? (
          <div className="pair-success">
            <strong>{t(($) => $.common.connected)}</strong>
            <p>{t(($) => $.pair.ready, { name: account.label })}</p>
            <Button type="button" variant="primary" onClick={onClose}>
              {t(($) => $.pair.openChats)}
            </Button>
          </div>
        ) : attempt.data?.state === "failed" ? (
          <div className="pair-error">
            <p>{t(($) => $.pair.failed)}</p>
            <Button
              type="button"
              onClick={() => {
                setAttemptID(undefined);
                start.mutate();
              }}
            >
              {t(($) => $.common.retry)}
            </Button>
          </div>
        ) : attempt.data?.challenge?.type === "qr" ? (
          <div className="qr-frame">
            <QRCodeSVG
              value={attempt.data.challenge.value}
              size={224}
              includeMargin
            />
            <p>{t(($) => $.pair.scan)}</p>
          </div>
        ) : (
          <Loading label={t(($) => $.pair.waiting)} />
        )}
      </div>
    </Dialog>
  );
}
