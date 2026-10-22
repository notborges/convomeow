import { useTranslation } from "react-i18next";
import type { GroupParticipantResult } from "../../api/types";

export function ParticipantResults({
  results,
}: {
  results: GroupParticipantResult[];
}) {
  const { t } = useTranslation();
  if (results.length === 0) return null;
  return (
    <ul className="participant-results">
      {results.map((result) => (
        <li key={result.provider_id}>
          <strong>
            {result.display_name || result.provider_id.split("@")[0]}
          </strong>
          <span>{t(($) => $.groups.status[result.status])}</span>
        </li>
      ))}
    </ul>
  );
}
