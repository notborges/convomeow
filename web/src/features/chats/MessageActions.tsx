import { Popover } from "@base-ui/react/popover";
import {
  Add01Icon,
  ArrowDown01Icon,
  ArrowTurnBackwardIcon,
  Cancel01Icon,
  Copy01Icon,
  InformationCircleIcon,
  SmileIcon,
} from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Message } from "../../api/types";
import { ActionMenu } from "../../ui/ActionMenu";
import { IconButton } from "../../ui/Button";
import { EmojiPicker } from "../../ui/EmojiPicker";

const quickEmoji = ["👍", "❤️", "😂", "😮", "😢", "🙏"];

export function MessageActions({
  message,
  onReply,
  onInfo,
  onReact,
  busy,
}: {
  message: Message;
  onReply: (message: Message) => void;
  onInfo: () => void;
  onReact: (emoji: string) => void;
  busy: boolean;
}) {
  const { t } = useTranslation();
  const trigger = useRef<HTMLButtonElement>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">(
    "idle",
  );
  const content = message.content.text || message.content.caption;
  const own = message.reactions?.find((item) => item.own)?.emoji;
  const select = (emoji: string) => {
    setPickerOpen(false);
    onReact(emoji === own ? "" : emoji);
  };
  return (
    <>
      <ActionMenu
        className="message-actions"
        triggerRef={trigger}
        label={t(($) => $.message.actions)}
        align="start"
        side={message.direction === "outbound" ? "left" : "right"}
        onOpen={() => setCopyState("idle")}
        triggerIcon={<HugeiconsIcon icon={ArrowDown01Icon} size={19} />}
        quickActions={[
          ...quickEmoji.map((emoji) => ({
            id: emoji,
            label: t(($) => $.reactions.chooseEmoji, { emoji }),
            icon: <span aria-hidden="true">{emoji}</span>,
            selected: emoji === own,
            disabled: busy,
            onSelect: () => select(emoji),
          })),
          {
            id: "more",
            label: t(($) => $.reactions.moreEmoji),
            icon: <HugeiconsIcon icon={Add01Icon} size={20} />,
            disabled: busy,
            movesFocus: true,
            onSelect: () => setPickerOpen(true),
          },
        ]}
        actions={[
          {
            id: "reply",
            label: t(($) => $.reply.action),
            icon: <HugeiconsIcon icon={ArrowTurnBackwardIcon} size={20} />,
            movesFocus: true,
            onSelect: () => onReply(message),
          },
          {
            id: "react",
            label: t(($) => $.reactions.action),
            icon: <HugeiconsIcon icon={SmileIcon} size={20} />,
            movesFocus: true,
            disabled: busy,
            onSelect: () => setPickerOpen(true),
          },
          ...(own
            ? [
                {
                  id: "remove-reaction",
                  label: t(($) => $.reactions.remove),
                  icon: <span aria-hidden="true">{own}</span>,
                  disabled: busy,
                  onSelect: () => onReact(""),
                },
              ]
            : []),
          ...(message.direction === "outbound"
            ? [
                {
                  id: "info",
                  label: t(($) => $.receipts.info),
                  icon: (
                    <HugeiconsIcon icon={InformationCircleIcon} size={20} />
                  ),
                  movesFocus: true,
                  onSelect: onInfo,
                },
              ]
            : []),
          ...(content
            ? [
                {
                  id: "copy",
                  label:
                    copyState === "copied"
                      ? t(($) => $.message.copied)
                      : copyState === "failed"
                        ? t(($) => $.message.copyFailed)
                        : t(($) => $.message.copy),
                  icon: <HugeiconsIcon icon={Copy01Icon} size={20} />,
                  closeOnSelect: false,
                  onSelect: async () => {
                    try {
                      await navigator.clipboard.writeText(content);
                      setCopyState("copied");
                    } catch {
                      setCopyState("failed");
                    }
                  },
                },
              ]
            : []),
        ]}
      />
      <Popover.Root open={pickerOpen} onOpenChange={setPickerOpen}>
        <Popover.Portal>
          <Popover.Positioner
            anchor={trigger}
            side={message.direction === "outbound" ? "left" : "right"}
            align="start"
            sideOffset={8}
            collisionPadding={12}
            className="emoji-popover-positioner"
          >
            <Popover.Popup
              className="emoji-popover"
              aria-label={t(($) => $.reactions.choose)}
              finalFocus={trigger}
            >
              <div className="emoji-popover__header">
                <span>{t(($) => $.reactions.choose)}</span>
                <IconButton
                  className="emoji-popover__close"
                  label={t(($) => $.common.close)}
                  onClick={() => setPickerOpen(false)}
                >
                  <HugeiconsIcon icon={Cancel01Icon} size={18} />
                </IconButton>
              </div>
              <EmojiPicker onSelect={select} />
            </Popover.Popup>
          </Popover.Positioner>
        </Popover.Portal>
      </Popover.Root>
    </>
  );
}
