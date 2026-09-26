import i18n from ".";
import en from "./locales/en.json";

export function formatDate(
  value: string | Date,
  options: Intl.DateTimeFormatOptions,
) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(i18n.resolvedLanguage ?? "en", options).format(
    date,
  );
}

export function kindLabel(kind: string) {
  return Object.hasOwn(en.kinds, kind)
    ? i18n.t(($) => $.kinds[kind as keyof typeof en.kinds])
    : i18n.t(($) => $.message.unknown);
}
export function stateLabel(state: string) {
  return Object.hasOwn(en.states, state)
    ? i18n.t(($) => $.states[state as keyof typeof en.states])
    : i18n.t(($) => $.states.unknown);
}
export function connectionLabel(state: string) {
  return state === "connected"
    ? i18n.t(($) => $.common.connected)
    : state === "pairing"
      ? i18n.t(($) => $.common.pairing)
      : i18n.t(($) => $.common.disconnected);
}
