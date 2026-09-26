import { Cancel01Icon, Search01Icon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { type InputHTMLAttributes, useId } from "react";
import { useTranslation } from "react-i18next";
import { IconButton } from "./Button";

type TextFieldProps = InputHTMLAttributes<HTMLInputElement> & {
  label: string;
  hint?: string;
  error?: string;
};
export function TextField({
  label,
  hint,
  error,
  id,
  ...props
}: TextFieldProps) {
  const generatedID = useId();
  const fieldID = id ?? generatedID;
  return (
    <div className="field">
      <label htmlFor={fieldID}>{label}</label>
      <input
        {...props}
        className="input"
        id={fieldID}
        aria-invalid={!!error}
        aria-describedby={error || hint ? `${fieldID}-description` : undefined}
      />
      {(error || hint) && (
        <p
          id={`${fieldID}-description`}
          className={error ? "form-error" : "field__hint"}
          role={error ? "alert" : undefined}
        >
          {error || hint}
        </p>
      )}
    </div>
  );
}

export function SearchField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const { t } = useTranslation();

  const id = useId();
  return (
    <div className="search-field">
      <HugeiconsIcon icon={Search01Icon} size={20} aria-hidden="true" />
      <label className="sr-only" htmlFor={id}>
        {label}
      </label>
      <input
        id={id}
        type="search"
        placeholder={label}
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
      {value && (
        <IconButton
          label={t(($) => $.common.clearSearch)}
          onClick={() => onChange("")}
        >
          <HugeiconsIcon icon={Cancel01Icon} size={16} />
        </IconButton>
      )}
    </div>
  );
}
