import { useTranslation } from "react-i18next";
export function Loading({ label }: { label?: string }) {
  const { t } = useTranslation();
  return (
    <div className="loading" role="status">
      <span className="loading__mark" />
      {label ?? t(($) => $.common.loading)}
    </div>
  );
}
