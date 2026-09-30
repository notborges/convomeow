import { useEffect, useState, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";
import { NotificationSubscription } from "./subscription";

export function useBrowserNotifications(authenticated: boolean) {
  const { i18n } = useTranslation();
  const [subscription] = useState(() => new NotificationSubscription());
  const state = useSyncExternalStore(
    subscription.subscribeToState,
    subscription.getSnapshot,
  );
  const locale = i18n.resolvedLanguage === "pt-BR" ? "pt-BR" : "en";

  useEffect(() => subscription.listen(), [subscription]);
  useEffect(() => {
    void subscription.updateSession(authenticated, locale);
  }, [subscription, authenticated, locale]);

  return {
    ...state,
    enable: subscription.enable,
    disable: subscription.disable,
    setPreview: subscription.setPreview,
  };
}

export type BrowserNotifications = ReturnType<typeof useBrowserNotifications>;
