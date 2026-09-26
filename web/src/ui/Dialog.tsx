import { Cancel01Icon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { motion } from "motion/react";
import {
  type PointerEvent,
  type ReactNode,
  useEffect,
  useId,
  useRef,
} from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { IconButton } from "./Button";

function isBackdrop(event: PointerEvent<HTMLDialogElement>) {
  if (event.target !== event.currentTarget) return false;
  const { left, right, top, bottom } =
    event.currentTarget.getBoundingClientRect();
  return (
    event.clientX < left ||
    event.clientX > right ||
    event.clientY < top ||
    event.clientY > bottom
  );
}

export function Dialog({
  title,
  description,
  children,
  onClose,
  className = "",
  actions,
}: {
  title: string;
  className?: string;
  actions?: ReactNode;
  description?: string;
  children: ReactNode;
  onClose: () => void;
}) {
  const { t } = useTranslation();

  const ref = useRef<HTMLDialogElement>(null);
  const backdropPointer = useRef<number | null>(null);
  const id = useId();
  useEffect(() => {
    const dialog = ref.current;
    dialog?.showModal();
    return () => dialog?.close();
  }, []);
  return createPortal(
    <motion.dialog
      initial={{ opacity: 0, y: 12, scale: 0.97 }}
      animate={{ opacity: 1, y: 0, scale: 1 }}
      className={`dialog ${className}`}
      ref={ref}
      aria-labelledby={`${id}-title`}
      aria-describedby={description ? `${id}-description` : undefined}
      onClose={onClose}
      onPointerDown={(event) => {
        backdropPointer.current =
          event.button === 0 && isBackdrop(event) ? event.pointerId : null;
      }}
      onPointerUp={(event) => {
        const dismiss =
          backdropPointer.current === event.pointerId && isBackdrop(event);
        backdropPointer.current = null;
        if (dismiss) event.currentTarget.close();
      }}
      onPointerCancel={() => {
        backdropPointer.current = null;
      }}
    >
      <header className="dialog__header">
        <div>
          <h2 id={`${id}-title`}>{title}</h2>
          {description && <p id={`${id}-description`}>{description}</p>}
        </div>
        {actions}
        <IconButton
          label={t(($) => $.common.close)}
          onClick={() => ref.current?.close()}
        >
          <HugeiconsIcon icon={Cancel01Icon} size={20} />
        </IconButton>
      </header>
      <div className="dialog__body">{children}</div>
    </motion.dialog>,
    document.body,
  );
}
