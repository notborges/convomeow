import { Menu } from "@base-ui/react/menu";
import { MoreHorizontalIcon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { type ReactNode, type Ref, useRef } from "react";
import { IconButton } from "./Button";

type Action = {
  id: string;
  label: string;
  icon: ReactNode;
  onSelect: () => void;
  movesFocus?: boolean;
  closeOnSelect?: boolean;
  disabled?: boolean;
  selected?: boolean;
};

export function ActionMenu({
  label,
  actions,
  className,
  align = "end",
  triggerIcon,
  side = "bottom",
  onOpen,
  quickActions = [],
  triggerRef,
}: {
  label: string;
  actions: Action[];
  className?: string;
  align?: "start" | "end";
  triggerIcon?: ReactNode;
  side?: "left" | "right" | "bottom";
  onOpen?: () => void;
  quickActions?: Action[];
  triggerRef?: Ref<HTMLButtonElement>;
}) {
  const restoreFocus = useRef(true);
  if (actions.length === 0) return null;
  return (
    <Menu.Root
      modal={false}
      onOpenChange={(open) => {
        if (open) {
          restoreFocus.current = true;
          onOpen?.();
        }
      }}
    >
      <Menu.Trigger
        render={
          <IconButton ref={triggerRef} label={label} className={className} />
        }
      >
        {triggerIcon ?? <HugeiconsIcon icon={MoreHorizontalIcon} size={18} />}
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner
          className="action-menu-positioner"
          positionMethod="fixed"
          side={side}
          sideOffset={8}
          align={align}
        >
          <Menu.Popup
            className="action-menu"
            finalFocus={() => restoreFocus.current}
          >
            {quickActions.length > 0 && (
              <Menu.Group className="action-menu__quick">
                {quickActions.map((action) => (
                  <Menu.Item
                    key={action.id}
                    className="action-menu__quick-item"
                    aria-label={action.label}
                    title={action.label}
                    disabled={action.disabled}
                    data-selected={action.selected || undefined}
                    onClick={() => {
                      restoreFocus.current = !action.movesFocus;
                      action.onSelect();
                    }}
                  >
                    {action.icon}
                  </Menu.Item>
                ))}
              </Menu.Group>
            )}
            <div className="action-menu__list">
              {actions.map((action) => (
                <Menu.Item
                  key={action.id}
                  className="action-menu__item"
                  disabled={action.disabled}
                  closeOnClick={action.closeOnSelect ?? true}
                  onClick={() => {
                    restoreFocus.current = !action.movesFocus;
                    action.onSelect();
                  }}
                >
                  <span className="action-menu__icon" aria-hidden="true">
                    {action.icon}
                  </span>
                  <span>{action.label}</span>
                </Menu.Item>
              ))}
            </div>
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  );
}
