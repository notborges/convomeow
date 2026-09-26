import {
  Attachment01Icon,
  Cancel01Icon,
  SendHorizontalIcon,
} from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useQueryClient } from "@tanstack/react-query";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import {
  type ChangeEvent,
  type FormEvent,
  type KeyboardEvent,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { Trans, useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { keys } from "../../api/queries";
import { type ErrorKey, errorKey } from "../../i18n/errors";
import { IconButton } from "../../ui/Button";
import { motionTiming } from "../../ui/motion";

function mediaKind(file: File): string {
  if (file.type.startsWith("image/")) return "image";
  if (file.type.startsWith("video/")) return "video";
  if (file.type.startsWith("audio/")) return "audio";
  return "document";
}

export function Composer({
  accountID,
  conversationID,
  connected,
}: {
  accountID: string;
  conversationID: string;
  connected: boolean;
}) {
  const { t } = useTranslation();

  const reducedMotion = useReducedMotion();
  const attachButton = useRef<HTMLButtonElement>(null);
  const [text, setText] = useState("");
  const [file, setFile] = useState<File>();
  const [previewURL, setPreviewURL] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ErrorKey>();
  const input = useRef<HTMLInputElement>(null);
  const textarea = useRef<HTMLTextAreaElement>(null);
  const retry = useRef<
    | {
        text: string;
        file?: File;
        uploadID?: string;
        key: string;
      }
    | undefined
  >(undefined);
  const queryClient = useQueryClient();

  useEffect(() => {
    if (!file?.type.startsWith("image/")) {
      setPreviewURL(undefined);
      return;
    }
    const url = URL.createObjectURL(file);
    setPreviewURL(url);
    return () => URL.revokeObjectURL(url);
  }, [file]);

  useLayoutEffect(() => {
    if (!textarea.current) return;
    textarea.current.style.height = "44px";
    textarea.current.style.height = `${Math.min(textarea.current.scrollHeight, 140)}px`;
  }, [text]);

  function pick(event: ChangeEvent<HTMLInputElement>) {
    setFile(event.target.files?.[0]);
    retry.current = undefined;
    setError(undefined);
  }

  async function submit(event?: FormEvent) {
    event?.preventDefault();
    const content = text.trim();
    if ((!content && !file) || !connected || busy) return;
    if (file && mediaKind(file) === "audio" && content) {
      setError("audioCaption");
      return;
    }
    const previous = retry.current;
    const attempt =
      previous?.text === content && previous.file === file
        ? previous
        : { text: content, file, key: crypto.randomUUID() };
    retry.current = attempt;
    setBusy(true);
    setError(undefined);
    try {
      if (file) {
        if (!attempt.uploadID)
          attempt.uploadID = (await api.upload(accountID, file)).id;
        await api.sendMedia(
          conversationID,
          mediaKind(file),
          attempt.uploadID,
          content,
          attempt.key,
        );
      } else {
        await api.sendText(conversationID, content, attempt.key);
      }
      retry.current = undefined;
      setText("");
      setFile(undefined);
      if (input.current) input.current.value = "";
      queryClient.invalidateQueries({
        queryKey: keys.messages(conversationID),
      });
      queryClient.invalidateQueries({
        queryKey: keys.conversations(accountID),
      });
    } catch (cause) {
      setError(errorKey(cause));
    } finally {
      setBusy(false);
    }
  }

  function keyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (
      event.key === "Enter" &&
      !event.shiftKey &&
      !event.nativeEvent.isComposing
    ) {
      event.preventDefault();
      void submit();
    }
  }

  return (
    <>
      <form className="composer" onSubmit={submit}>
        <AnimatePresence initial={false}>
          {file && (
            <motion.div
              key="attachment"
              className="composer-attachment"
              initial={{ opacity: 0, height: reducedMotion ? "auto" : 0 }}
              animate={{ opacity: 1, height: "auto" }}
              exit={{ opacity: 0, height: reducedMotion ? "auto" : 0 }}
              transition={motionTiming.feedback}
            >
              <div className="composer-file">
                {previewURL ? (
                  <img
                    className="composer-file__preview"
                    src={previewURL}
                    alt={t(($) => $.composer.preview)}
                  />
                ) : (
                  <HugeiconsIcon icon={Attachment01Icon} size={20} />
                )}
                <span>{file.name}</span>
                <IconButton
                  label={t(($) => $.composer.remove)}
                  disabled={busy}
                  onClick={() => {
                    attachButton.current?.focus();
                    setFile(undefined);
                    retry.current = undefined;
                    if (input.current) input.current.value = "";
                  }}
                >
                  <HugeiconsIcon icon={Cancel01Icon} size={16} />
                </IconButton>
              </div>
            </motion.div>
          )}
        </AnimatePresence>
        <div className="composer-row">
          <IconButton
            ref={attachButton}
            label={t(($) => $.composer.attach)}
            disabled={!connected || busy}
            onClick={() => input.current?.click()}
          >
            <HugeiconsIcon icon={Attachment01Icon} size={21} />
          </IconButton>
          <input
            ref={input}
            type="file"
            className="file-input"
            onChange={pick}
            disabled={!connected || busy}
            tabIndex={-1}
          />
          <textarea
            ref={textarea}
            rows={1}
            placeholder={
              connected
                ? file
                  ? t(($) => $.composer.caption)
                  : t(($) => $.composer.write)
                : t(($) => $.composer.disconnected)
            }
            aria-label={t(($) => $.composer.message)}
            value={text}
            onChange={(event) => setText(event.target.value)}
            onKeyDown={keyDown}
            disabled={!connected || busy}
          />
          <IconButton
            type="submit"
            variant="primary"
            busy={busy}
            label={t(($) => $.composer.send)}
            disabled={(!text.trim() && !file) || !connected || busy}
          >
            <HugeiconsIcon icon={SendHorizontalIcon} size={20} />
          </IconButton>
        </div>
        {error && (
          <p className="composer-error" role="alert">
            {t(($) => $.errors[error])}
          </p>
        )}
      </form>
      <p className="composer-hint">
        {connected ? (
          <Trans
            i18nKey={($) => $.composer.shortcut}
            components={{ enter: <kbd />, shift: <kbd /> }}
          />
        ) : (
          t(($) => $.composer.reconnect)
        )}
      </p>
    </>
  );
}
