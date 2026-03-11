import { useId } from "react";
import Radio from "@/shared/components/Radio";

export interface InlineRadioOption<ValueType extends number | string> {
  value: ValueType;
  label: string;
  testId: string;
}

interface InlineRadioGroupProps<ValueType extends number | string> {
  label: string;
  value: ValueType;
  options: InlineRadioOption<ValueType>[];
  onChange: (nextValue: ValueType) => void;
}

const InlineRadioGroup = <ValueType extends number | string>({
  label,
  value,
  options,
  onChange,
}: InlineRadioGroupProps<ValueType>) => {
  const groupName = useId();

  return (
    <fieldset className="w-full space-y-2">
      <legend className="text-emphasis-300 text-text-primary">{label}</legend>
      <div role="radiogroup" aria-label={label} className="flex flex-wrap gap-x-6 gap-y-0">
        {options.map((option) => {
          const selected = option.value === value;

          return (
            <label
              key={option.value}
              aria-label={option.label}
              data-testid={option.testId}
              className="flex h-12 cursor-pointer items-center gap-2 text-300 text-text-primary"
            >
              <Radio
                name={groupName}
                value={String(option.value)}
                selected={selected}
                onChange={() => onChange(option.value)}
              />
              <span>{option.label}</span>
            </label>
          );
        })}
      </div>
    </fieldset>
  );
};

export default InlineRadioGroup;
