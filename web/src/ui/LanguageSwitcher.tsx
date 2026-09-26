import { LanguageCircleIcon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { languages } from "../i18n";
import { IconButton } from "./Button";
import { Dialog } from "./Dialog";

export function LanguageSwitcher({ rail = false }: { rail?: boolean }) {
  const { t, i18n } = useTranslation();
  const [open, setOpen] = useState(false);
  return (
    <>
      <IconButton
        label={t(($) => $.common.language)}
        className={rail ? "rail-action" : undefined}
        onClick={() => setOpen(true)}
      >
        <HugeiconsIcon icon={LanguageCircleIcon} size={22} />
      </IconButton>
      {open && (
        <Dialog
          title={t(($) => $.common.language)}
          onClose={() => setOpen(false)}
        >
          <div className="language-options">
            {languages.map((language) => (
              <label key={language.code}>
                <input
                  type="radio"
                  name="language"
                  value={language.code}
                  checked={i18n.resolvedLanguage === language.code}
                  onChange={() => void i18n.changeLanguage(language.code)}
                />
                <span lang={language.code}>{language.label}</span>
              </label>
            ))}
          </div>
        </Dialog>
      )}
    </>
  );
}
