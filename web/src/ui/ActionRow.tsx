import type { ComponentPropsWithRef, ReactNode } from "react";

export function ActionRow({
  icon,
  children,
  tone = "accent",
  className = "",
  type = "button",
  ...props
}: ComponentPropsWithRef<"button"> & {
  icon: ReactNode;
  tone?: "accent" | "danger";
}) {
  return (
    <button
      {...props}
      type={type}
      className={`action-row action-row--${tone} ${className}`}
    >
      <span className="action-row__icon" aria-hidden="true">
        {icon}
      </span>
      <span className="action-row__label">{children}</span>
    </button>
  );
}
