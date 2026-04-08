import { useCallback, useLayoutEffect, useRef, useState } from "react";
import clsx from "clsx";

import Calendar from "./Calendar";
import { PresetId } from "./constants";
import DatePickerInput from "./DatePickerInput";
import PresetList from "./PresetList";
import { DatePickerProps } from "./types";
import {
  computePresetDates,
  formatDisplayDate,
  formatDisplayDateTime,
  isDateInTimeframe,
  isDateRangeSelectable,
  isSameDay,
} from "./utils";
import { Calendar as CalendarIcon } from "@/shared/assets/icons";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

const hasOwnProperty = <K extends keyof DatePickerProps>(props: DatePickerProps, key: K): boolean =>
  Object.prototype.hasOwnProperty.call(props, key);

const DatePicker = (props: DatePickerProps) => {
  const {
    selectionMode = "single",
    withInputs = false,
    displayMenu = false,
    timeframe,
    weekStartsOn = 0,
    isDateDisabled,
    selectedDate: controlledDate,
    selectedStartDate: controlledStartDate,
    selectedEndDate: controlledEndDate,
    onSelectedDateChange,
    onSelectedDateRangeChange,
    presets: customPresets,
    className,
    testId = "date-picker",
    disabled = false,
  } = props;

  const isDateControlled = hasOwnProperty(props, "selectedDate");
  const isStartControlled = hasOwnProperty(props, "selectedStartDate");
  const isEndControlled = hasOwnProperty(props, "selectedEndDate");

  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  // Internal state (used when not fully controlled)
  const [internalDate, setInternalDate] = useState<Date | undefined>(controlledDate);
  const [internalStartDate, setInternalStartDate] = useState<Date | undefined>(controlledStartDate);
  const [internalEndDate, setInternalEndDate] = useState<Date | undefined>(controlledEndDate);

  const selectedDate = isDateControlled ? controlledDate : internalDate;
  const selectedStartDate = isStartControlled ? controlledStartDate : internalStartDate;
  const selectedEndDate = isEndControlled ? controlledEndDate : internalEndDate;

  // Calendar navigation
  const initialMonth = selectedDate ?? selectedStartDate ?? selectedEndDate ?? new Date();
  const initialSelectionKey =
    selectionMode === "single" ? selectedDate?.getTime() : (selectedStartDate ?? selectedEndDate)?.getTime();
  const [calendarView, setCalendarView] = useState(() => ({
    month: initialMonth.getMonth(),
    year: initialMonth.getFullYear(),
    selectionKey: initialSelectionKey,
  }));

  // Range hover preview
  const [hoveredDate, setHoveredDate] = useState<Date | undefined>();

  // Track whether we're waiting for the second range click
  const [rangeSelecting, setRangeSelecting] = useState(false);
  const [pendingRangeStartDate, setPendingRangeStartDate] = useState<Date | undefined>();

  // Active preset tracking
  const [activePreset, setActivePreset] = useState<PresetId | string | undefined>();

  const resetPendingRangeState = useCallback(() => {
    setPendingRangeStartDate(undefined);
    setHoveredDate(undefined);
    setRangeSelecting(false);
  }, []);

  const closePicker = useCallback(() => {
    resetPendingRangeState();
    setOpen(false);
  }, [resetPendingRangeState]);

  useClickOutside({
    ref: containerRef,
    onClickOutside: closePicker,
  });

  // Disabled is an external control boundary, so close the panel before paint when it flips on.
  useLayoutEffect(() => {
    if (!disabled) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect
    closePicker();
  }, [disabled, closePicker]);

  const isControlledSelection = selectionMode === "single" ? isDateControlled : isStartControlled || isEndControlled;
  const controlledSelection = selectionMode === "single" ? selectedDate : (selectedStartDate ?? selectedEndDate);
  const controlledSelectionKey = controlledSelection?.getTime();
  const syncedSelectionDate = controlledSelection ?? new Date();
  const syncedCalendarView =
    isControlledSelection && calendarView.selectionKey !== controlledSelectionKey
      ? {
          month: syncedSelectionDate.getMonth(),
          year: syncedSelectionDate.getFullYear(),
          selectionKey: controlledSelectionKey,
        }
      : calendarView;
  const displayedMonth = syncedCalendarView.month;
  const displayedYear = syncedCalendarView.year;

  const navigateMonth = useCallback(
    (increment: number) => {
      const nextDate = new Date(displayedYear, displayedMonth + increment, 1);
      setCalendarView({
        month: nextDate.getMonth(),
        year: nextDate.getFullYear(),
        selectionKey: controlledSelectionKey,
      });
    },
    [displayedMonth, displayedYear, controlledSelectionKey],
  );

  const navigateToDate = useCallback((date: Date) => {
    setCalendarView({
      month: date.getMonth(),
      year: date.getFullYear(),
      selectionKey: date.getTime(),
    });
  }, []);

  const isSelectableDate = useCallback(
    (date: Date) => isDateInTimeframe(date, timeframe) && !isDateDisabled?.(date),
    [timeframe, isDateDisabled],
  );

  const applySingleDate = useCallback(
    (date: Date): boolean => {
      if (!isSelectableDate(date)) return false;
      setInternalDate(date);
      navigateToDate(date);
      onSelectedDateChange?.(date);
      closePicker();
      return true;
    },
    [isSelectableDate, navigateToDate, onSelectedDateChange, closePicker],
  );

  const applyRange = useCallback(
    (start: Date, end: Date): boolean => {
      if (start.getTime() > end.getTime()) return false;
      if (!isDateRangeSelectable(start, end, isSelectableDate)) return false;

      setInternalStartDate(start);
      setInternalEndDate(end);
      resetPendingRangeState();
      navigateToDate(start);
      onSelectedDateRangeChange?.(start, end);
      return true;
    },
    [isSelectableDate, resetPendingRangeState, navigateToDate, onSelectedDateRangeChange],
  );

  const beginRangeSelection = useCallback(
    (date: Date): boolean => {
      if (!isSelectableDate(date)) return false;
      setPendingRangeStartDate(date);
      setHoveredDate(undefined);
      setRangeSelecting(true);
      return true;
    },
    [isSelectableDate],
  );

  const activeStartDate = rangeSelecting ? (pendingRangeStartDate ?? selectedStartDate) : selectedStartDate;
  const activeEndDate = rangeSelecting ? (pendingRangeStartDate ?? selectedStartDate) : selectedEndDate;

  const handleDayClick = useCallback(
    (date: Date) => {
      setActivePreset(undefined);

      if (selectionMode === "single") {
        applySingleDate(date);
        return;
      }

      const rangeStart = pendingRangeStartDate ?? selectedStartDate;
      if (!rangeSelecting || !rangeStart) {
        beginRangeSelection(date);
        return;
      }

      if (date.getTime() >= rangeStart.getTime()) {
        if (applyRange(rangeStart, date)) {
          return;
        }
      }

      beginRangeSelection(date);
    },
    [
      selectionMode,
      rangeSelecting,
      pendingRangeStartDate,
      selectedStartDate,
      applySingleDate,
      applyRange,
      beginRangeSelection,
    ],
  );

  const handlePresetClick = useCallback(
    (presetId: PresetId) => {
      if (presetId === "custom") {
        setActivePreset("custom");
        return;
      }

      const dates = computePresetDates(presetId, weekStartsOn);
      if (!dates) return;

      if (selectionMode === "single") {
        if (applySingleDate(dates.startDate)) {
          setActivePreset(presetId);
        }
      } else if (applyRange(dates.startDate, dates.endDate)) {
        setActivePreset(presetId);
      }
    },
    [selectionMode, weekStartsOn, applySingleDate, applyRange],
  );

  const handleCustomPresetClick = useCallback(
    (preset: { label: string; startDate: Date; endDate: Date }) => {
      if (selectionMode === "single") {
        if (applySingleDate(preset.startDate)) {
          setActivePreset(preset.label);
        }
      } else if (applyRange(preset.startDate, preset.endDate)) {
        setActivePreset(preset.label);
      }
    },
    [selectionMode, applySingleDate, applyRange],
  );

  const handleInputDateChange = useCallback(
    (date: Date) => {
      setActivePreset(undefined);
      applySingleDate(date);
    },
    [applySingleDate],
  );

  const handleInputRangeChange = useCallback(
    (start: Date, end: Date) => {
      setActivePreset(undefined);
      applyRange(start, end);
    },
    [applyRange],
  );

  const includeTime = withInputs === "date-and-time";
  const formatTriggerDate = includeTime ? formatDisplayDateTime : formatDisplayDate;

  // Trigger display text
  const triggerText = (() => {
    if (selectionMode === "single") {
      return selectedDate ? formatTriggerDate(selectedDate) : "Select date";
    }
    if (activeStartDate && activeEndDate && !rangeSelecting) {
      const isCollapsedRange = includeTime
        ? activeStartDate.getTime() === activeEndDate.getTime()
        : isSameDay(activeStartDate, activeEndDate);
      if (isCollapsedRange) {
        return formatTriggerDate(activeStartDate);
      }
      return `${formatTriggerDate(activeStartDate)} — ${formatTriggerDate(activeEndDate)}`;
    }
    if (activeStartDate) {
      return `${formatTriggerDate(activeStartDate)} — ...`;
    }
    return "Select dates";
  })();

  const hasValue = selectionMode === "single" ? !!selectedDate : !!activeStartDate;

  return (
    <div ref={containerRef} className={clsx("relative inline-block", className)} data-testid={testId}>
      {/* Trigger */}
      <button
        type="button"
        className={clsx(
          "flex w-full min-w-[280px] items-center gap-2 rounded-lg border px-4 py-2.5 text-left text-300 transition duration-200 ease-in-out",
          {
            "border-border-5 bg-surface-base": !disabled && !open,
            "border-border-20 bg-surface-base ring-4 ring-surface-10": open && !disabled,
            "cursor-not-allowed border-border-5 bg-core-primary-5 text-text-primary-50": disabled,
            "cursor-pointer hover:border-border-20": !disabled,
          },
        )}
        onClick={() => {
          if (disabled) return;
          if (open) {
            closePicker();
            return;
          }
          setOpen(true);
        }}
        disabled={disabled}
        aria-haspopup="dialog"
        aria-expanded={open && !disabled}
        data-testid={`${testId}-trigger`}
      >
        <CalendarIcon className="text-text-primary-50" width="w-4" />
        <span className={hasValue ? "text-text-primary" : "text-text-primary-50"}>{triggerText}</span>
      </button>

      {/* Dropdown panel */}
      {open && !disabled && (
        <div
          className={clsx(
            "absolute left-0 z-50 mt-2 rounded-3xl bg-surface-elevated-base/85 p-6 shadow-200 backdrop-blur-[7px]",
            "animate-slide-down-popover",
          )}
          data-testid={`${testId}-panel`}
        >
          <div className="flex gap-4">
            {/* Preset menu */}
            {displayMenu && (
              <PresetList
                activePreset={activePreset}
                timeframe={timeframe}
                customPresets={customPresets}
                onPresetClick={handlePresetClick}
                onCustomPresetClick={handleCustomPresetClick}
                testId={`${testId}-presets`}
              />
            )}

            {/* Calendar + optional inputs */}
            <div className="flex flex-col">
              {withInputs && (
                <DatePickerInput
                  selectionMode={selectionMode}
                  withInputs={withInputs}
                  selectedDate={selectedDate}
                  selectedStartDate={activeStartDate}
                  selectedEndDate={activeEndDate}
                  onDateChange={handleInputDateChange}
                  onRangeChange={handleInputRangeChange}
                  disabled={disabled}
                  testId={`${testId}-inputs`}
                />
              )}
              <Calendar
                displayedYear={displayedYear}
                displayedMonth={displayedMonth}
                selectionMode={selectionMode}
                selectedDate={selectedDate}
                selectedStartDate={activeStartDate}
                selectedEndDate={activeEndDate}
                hoveredDate={hoveredDate}
                weekStartsOn={weekStartsOn}
                timeframe={timeframe}
                isDateDisabled={isDateDisabled}
                onDayClick={handleDayClick}
                onDayHover={setHoveredDate}
                onNavigateMonth={navigateMonth}
                testId={`${testId}-calendar`}
              />
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default DatePicker;
