import clsx from "clsx";

import DatePicker from "./DatePicker";
import type { DatePickerLabelPlacement, DatePickerProps } from "./types";

interface DatePickerFieldProps extends Omit<
  DatePickerProps,
  | "className"
  | "floatingLabel"
  | "id"
  | "label"
  | "onSelectedDateChange"
  | "onSelectedDateRangeChange"
  | "selectedDate"
  | "selectedEndDate"
  | "selectedStartDate"
  | "selectionMode"
> {
  id: string;
  label: string;
  selectedDate?: Date;
  onSelectedDateChange?: (date: Date) => void;
  className?: string;
  clearable?: boolean;
  onClear?: () => void;
  labelPlacement?: DatePickerLabelPlacement;
}

const DatePickerField = ({
  id,
  label,
  selectedDate,
  onSelectedDateChange,
  error,
  className,
  clearable = false,
  onClear,
  labelPlacement = "above",
  disabled = false,
  ...props
}: DatePickerFieldProps) => {
  const showClearButton = clearable && !!selectedDate && !!onClear;

  return (
    <div className={clsx("relative", className)}>
      {labelPlacement === "above" || showClearButton ? (
        <div className="mb-1 flex items-center justify-between gap-2">
          {labelPlacement === "above" ? (
            <label htmlFor={id} className="block text-200 text-text-primary-50">
              {label}
            </label>
          ) : (
            <div />
          )}
          {showClearButton ? (
            <button
              type="button"
              onClick={onClear}
              disabled={disabled}
              className={clsx("text-200 transition", {
                "text-text-primary-70 hover:text-text-primary": !disabled,
                "cursor-not-allowed text-text-primary-50": disabled,
              })}
            >
              Clear
            </button>
          ) : null}
        </div>
      ) : null}
      <DatePicker
        {...props}
        id={id}
        label={labelPlacement === "floating" ? label : undefined}
        floatingLabel={labelPlacement === "floating"}
        selectionMode="single"
        selectedDate={selectedDate}
        onSelectedDateChange={onSelectedDateChange}
        error={error}
        disabled={disabled}
        className="w-full"
      />
      <div
        className={clsx(
          "text-200 text-intent-critical-fill",
          "transition-[opacity,max-height,margin-top] duration-200 ease-in-out",
          { "max-h-0 opacity-0": !error || error === true },
          { "mt-2 max-h-10 opacity-100": error && error !== true },
        )}
      >
        <div className="flex items-center space-x-1">
          <div className="h-1 w-2.5 rounded-full bg-intent-critical-20" />
          <div id={typeof error === "string" && error ? `${id}-error` : undefined}>{error}</div>
        </div>
      </div>
    </div>
  );
};

export default DatePickerField;
