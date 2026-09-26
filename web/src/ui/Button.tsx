import type { ComponentPropsWithRef } from "react";

type ButtonProps = ComponentPropsWithRef<"button"> & {
  variant?: "primary" | "secondary" | "ghost" | "text";
  busy?: boolean;
};

export function Button({
  variant = "secondary",
  busy = false,
  disabled,
  className = "",
  children,
  type = "button",
  ...props
}: ButtonProps) {
  return (
    <button
      {...props}
      type={type}
      className={`button button--${variant} ${className}`}
      disabled={disabled || busy}
      aria-busy={busy || undefined}
    >
      {busy && <span className="button-spinner" aria-hidden="true" />}
      {children}
    </button>
  );
}

export function IconButton({
  label,
  children,
  busy,
  variant = "ghost",
  className = "",
  ...props
}: Omit<ButtonProps, "aria-label"> & { label: string }) {
  return (
    <Button
      {...props}
      busy={busy}
      children={busy ? null : children}
      variant={variant}
      aria-label={label}
      title={label}
      className={`icon-button ${className}`}
    />
  );
}
