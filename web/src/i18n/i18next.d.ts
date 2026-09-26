import "i18next";
import type en from "./locales/en.json";

declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "translation";
    enableSelector: true;
    resources: { translation: typeof en };
  }
}
