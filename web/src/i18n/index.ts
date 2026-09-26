import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import en from "./locales/en.json";
import ptBR from "./locales/pt-BR.json";

export const languages = [
  { code: "en", label: "English" },
  { code: "pt-BR", label: "Português (Brasil)" },
] as const;

function savedLanguage() {
  try {
    const language = localStorage.getItem("convomeow-language");
    return languages.some(({ code }) => code === language)
      ? (language ?? "en")
      : "en";
  } catch {
    return "en";
  }
}

void i18n.use(initReactI18next).init({
  resources: { en: { translation: en }, "pt-BR": { translation: ptBR } },
  lng: savedLanguage(),
  fallbackLng: "en",
  supportedLngs: languages.map(({ code }) => code),
  interpolation: { escapeValue: false },
  initAsync: false,
});

function updateDocument(language: string) {
  document.documentElement.lang = language;
  document.documentElement.dir = i18n.dir(language);
  try {
    localStorage.setItem("convomeow-language", language);
  } catch {
    /* Language switching remains available without storage. */
  }
}
updateDocument(i18n.resolvedLanguage ?? "en");
i18n.on("languageChanged", updateDocument);

export default i18n;
