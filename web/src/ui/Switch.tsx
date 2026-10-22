import { useId } from "react";

export function Switch({
  label,
  description,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  description?: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (checked: boolean) => void;
}) {
  const id = useId();
  return (
    <div className="switch-row">
      <span className="switch-row__text">
        <label htmlFor={id}>{label}</label>
        {description && <small id={`${id}-description`}>{description}</small>}
      </span>
      <button
        id={id}
        type="button"
        role="switch"
        className="switch"
        aria-checked={checked}
        aria-describedby={description ? `${id}-description` : undefined}
        disabled={disabled}
        onClick={() => onChange(!checked)}
      >
        <span className="switch__thumb" aria-hidden="true" />
      </button>
    </div>
  );
}
