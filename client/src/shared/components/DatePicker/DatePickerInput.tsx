import { useCallback, useEffect, useState } from "react";
import clsx from "clsx";

import { SelectionMode, WithInputs } from "./types";
import { formatDate, formatTime, parseDate, parseDateTime } from "./utils";

interface DatePickerInputProps {
  selectionMode: SelectionMode;
  withInputs: WithInputs;
  selectedDate?: Date;
  selectedStartDate?: Date;
  selectedEndDate?: Date;
  onDateChange: (date: Date) => void;
  onRangeChange: (start: Date, end: Date) => void;
  disabled?: boolean;
  testId?: string;
}

const DatePickerInput = ({
  selectionMode,
  withInputs,
  selectedDate,
  selectedStartDate,
  selectedEndDate,
  onDateChange,
  onRangeChange,
  disabled,
  testId,
}: DatePickerInputProps) => {
  const includeTime = withInputs === "date-and-time";

  const formatValue = useCallback(
    (date?: Date): string => {
      if (!date) return "";
      return includeTime ? `${formatDate(date)} ${formatTime(date)}` : formatDate(date);
    },
    [includeTime],
  );

  const [dateValue, setDateValue] = useState(() => formatValue(selectedDate));
  const [startValue, setStartValue] = useState(() => formatValue(selectedStartDate));
  const [endValue, setEndValue] = useState(() => formatValue(selectedEndDate));

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- sync input display with external date prop when parent updates it
    setDateValue(formatValue(selectedDate));
  }, [selectedDate, formatValue]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- sync input display with external date prop when parent updates it
    setStartValue(formatValue(selectedStartDate));
  }, [selectedStartDate, formatValue]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- sync input display with external date prop when parent updates it
    setEndValue(formatValue(selectedEndDate));
  }, [selectedEndDate, formatValue]);

  const handleSingleBlur = useCallback(() => {
    const parsed = includeTime ? parseDateTime(dateValue) : parseDate(dateValue);
    if (parsed) onDateChange(parsed);
  }, [dateValue, includeTime, onDateChange]);

  const handleRangeBlur = useCallback(
    (field: "start" | "end") => {
      const parse = includeTime ? parseDateTime : parseDate;
      const start = parse(startValue);
      const end = parse(endValue);

      if (start && end) {
        if (start.getTime() <= end.getTime()) {
          onRangeChange(start, end);
        }
        return;
      }

      if (field === "start" && start && selectedEndDate && start.getTime() <= selectedEndDate.getTime()) {
        onRangeChange(start, selectedEndDate);
      } else if (field === "end" && end && selectedStartDate && selectedStartDate.getTime() <= end.getTime()) {
        onRangeChange(selectedStartDate, end);
      }
    },
    [startValue, endValue, includeTime, selectedStartDate, selectedEndDate, onRangeChange],
  );

  const inputClasses = clsx(
    "w-full rounded-lg border border-border-5 bg-surface-base px-3 py-2 text-300 text-text-primary outline-hidden",
    "transition duration-200 ease-in-out",
    "focus:border-border-20 focus:ring-4 focus:ring-surface-10",
    { "cursor-not-allowed bg-core-primary-5": disabled },
  );

  const placeholder = includeTime ? "YYYY-MM-DD HH:MM" : "YYYY-MM-DD";

  if (selectionMode === "single") {
    return (
      <div className="mb-3" data-testid={testId}>
        <label className="mb-1 block text-200 text-text-primary-50">Date</label>
        <input
          type="text"
          className={inputClasses}
          value={dateValue}
          onChange={(e) => setDateValue(e.target.value)}
          onBlur={handleSingleBlur}
          placeholder={placeholder}
          disabled={disabled}
          data-testid={testId ? `${testId}-date-input` : undefined}
        />
      </div>
    );
  }

  return (
    <div className="mb-3 flex gap-2" data-testid={testId}>
      <div className="flex-1">
        <label className="mb-1 block text-200 text-text-primary-50">Start Date</label>
        <input
          type="text"
          className={inputClasses}
          value={startValue}
          onChange={(e) => setStartValue(e.target.value)}
          onBlur={() => handleRangeBlur("start")}
          placeholder={placeholder}
          disabled={disabled}
          data-testid={testId ? `${testId}-start-input` : undefined}
        />
      </div>
      <div className="flex-1">
        <label className="mb-1 block text-200 text-text-primary-50">End Date</label>
        <input
          type="text"
          className={inputClasses}
          value={endValue}
          onChange={(e) => setEndValue(e.target.value)}
          onBlur={() => handleRangeBlur("end")}
          placeholder={placeholder}
          disabled={disabled}
          data-testid={testId ? `${testId}-end-input` : undefined}
        />
      </div>
    </div>
  );
};

export default DatePickerInput;
