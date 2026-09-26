import { Moon02Icon, Sun03Icon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { IconButton } from "./Button";

export function ThemeToggle() {
  const { t } = useTranslation();

  const [dark, setDark] = useState(
    document.documentElement.dataset.theme === "dark",
  );
  function toggle() {
    const next = !dark;
    setDark(next);
    document.documentElement.dataset.theme = next ? "dark" : "light";
    try {
      localStorage.setItem("convomeow-theme", next ? "dark" : "light");
    } catch {
      /* Theme changes still work when storage is unavailable. */
    }
  }
  return (
    <IconButton
      label={
        dark ? t(($) => $.common.lightTheme) : t(($) => $.common.darkTheme)
      }
      className="rail-action"
      onClick={toggle}
    >
      <HugeiconsIcon icon={dark ? Sun03Icon : Moon02Icon} size={22} />
    </IconButton>
  );
}
