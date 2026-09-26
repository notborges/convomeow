import {
  Chat01Icon,
  TelegramIcon,
  WhatsappIcon,
} from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";

const providers = {
  whatsapp: { name: "WhatsApp", icon: WhatsappIcon },
  telegram: { name: "Telegram", icon: TelegramIcon },
};

export function providerName(provider: string) {
  return providers[provider as keyof typeof providers]?.name ?? provider;
}

export function ProviderBadge({
  provider,
  compact = false,
}: {
  provider: string;
  compact?: boolean;
}) {
  const definition = providers[provider as keyof typeof providers];
  const name = definition?.name ?? provider;
  return (
    <span
      className={`provider-badge ${compact ? "provider-badge--compact" : ""}`}
      data-provider={provider}
      title={name}
    >
      <HugeiconsIcon
        icon={definition?.icon ?? Chat01Icon}
        size={compact ? 14 : 16}
        aria-hidden="true"
      />
      <span className={compact ? "sr-only" : undefined}>{name}</span>
    </span>
  );
}
