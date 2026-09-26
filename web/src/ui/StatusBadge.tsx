import { useTranslation } from "react-i18next";
export function StatusBadge({ state }: { state: string }) {
  const { t } = useTranslation();

  const connected = state === "connected";
  return (
    <span
      className={`status-badge ${connected ? "status-badge--connected" : ""}`}
    >
      <span className="status-dot" aria-hidden="true" />
      {connected
        ? t(($) => $.common.connected)
        : state === "pairing"
          ? t(($) => $.common.pairing)
          : t(($) => $.common.disconnected)}
    </span>
  );
}
