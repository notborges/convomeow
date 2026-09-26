import { LayoutGroup, motion } from "motion/react";
import { useId } from "react";
import { motionTiming } from "./motion";
export function SegmentedControl<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: readonly { value: T; label: string }[];
  onChange: (value: T) => void;
}) {
  const id = useId();
  return (
    <LayoutGroup id={id}>
      <fieldset className="segmented-control" aria-label={label}>
        {options.map((option) => (
          <button
            type="button"
            key={option.value}
            aria-pressed={option.value === value}
            onClick={() => onChange(option.value)}
          >
            {option.value === value && (
              <motion.span
                className="segmented-control__selection"
                layoutId="selection"
                transition={motionTiming.selection}
                aria-hidden="true"
              />
            )}
            <span className="segmented-control__label">{option.label}</span>
          </button>
        ))}
      </fieldset>
    </LayoutGroup>
  );
}
