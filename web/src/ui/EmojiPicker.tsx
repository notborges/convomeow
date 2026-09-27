import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "./Button";
import { Loading } from "./Loading";

export function EmojiPicker({
  onSelect,
}: {
  onSelect: (emoji: string) => void;
}) {
  const { t } = useTranslation();
  const host = useRef<HTMLDivElement>(null);
  const select = useRef(onSelect);
  select.current = onSelect;
  const [state, setState] = useState<"loading" | "ready" | "failed">("loading");
  const [attempt, setAttempt] = useState(0);
  const copy = useMemo(
    () => t(($) => $.emojiPicker, { returnObjects: true }),
    [t],
  );
  useEffect(() => {
    let disposed = false;
    const node = host.current;
    setState("loading");
    Promise.all([import("emoji-mart"), import("@emoji-mart/data")])
      .then(([{ Picker }, data]) => {
        if (disposed || !node) return;
        const picker = new Picker({
          data: data.default,
          i18n: copy,
          theme:
            document.documentElement.dataset.theme === "dark"
              ? "dark"
              : "light",
          set: "native",
          perLine: 8,
          dynamicWidth: true,
          navPosition: "bottom",
          maxFrequentRows: 1,
          emojiButtonSize: 36,
          previewPosition: "none",
          skinTonePosition: "search",
          autoFocus: window.matchMedia("(pointer: fine)").matches,
          onEmojiSelect: (emoji: { native: string }) =>
            select.current(emoji.native),
        });
        node.replaceChildren(picker as unknown as HTMLElement);
        setState("ready");
      })
      .catch(() => {
        if (!disposed) setState("failed");
      });
    return () => {
      disposed = true;
      node?.replaceChildren();
    };
  }, [copy, attempt]);
  return (
    <div className="emoji-picker">
      {state === "loading" && <Loading />}
      {state === "failed" && (
        <div role="alert">
          <p>{t(($) => $.reactions.pickerFailed)}</p>
          <Button onClick={() => setAttempt((value) => value + 1)}>
            {t(($) => $.common.retry)}
          </Button>
        </div>
      )}
      <div ref={host} />
    </div>
  );
}
