import { useCallback, useMemo } from "react";
import clsx from "clsx";

import { MONTH_NAMES } from "./constants";
import { DayData, DaySelection, Timeframe, WeekDay } from "./types";
import {
  buildCalendarGrid,
  getOrderedDayNames,
  isAfterDay,
  isBetweenDays,
  isDateInTimeframe,
  isSameDay,
} from "./utils";
import { ChevronDown } from "@/shared/assets/icons";

interface CalendarProps {
  displayedYear: number;
  displayedMonth: number;
  selectionMode: "single" | "range";
  selectedDate?: Date;
  selectedStartDate?: Date;
  selectedEndDate?: Date;
  hoveredDate?: Date;
  weekStartsOn: WeekDay;
  timeframe?: Timeframe;
  isDateDisabled?: (date: Date) => boolean;
  onDayClick: (date: Date) => void;
  onDayHover: (date: Date | undefined) => void;
  onNavigateMonth: (increment: number) => void;
  testId?: string;
}

function getDaySelection(
  day: DayData,
  selectionMode: "single" | "range",
  selectedDate?: Date,
  selectedStartDate?: Date,
  selectedEndDate?: Date,
  hoveredDate?: Date,
): DaySelection {
  if (selectionMode === "single") {
    if (selectedDate && isSameDay(day.dateObj, selectedDate)) return "single";
    return "none";
  }

  // Range mode
  const start = selectedStartDate;
  const end = selectedEndDate;

  if (!start) return "none";

  // Completed range (start and end are different)
  if (end && !isSameDay(start, end)) {
    if (isSameDay(day.dateObj, start)) return "range-first";
    if (isSameDay(day.dateObj, end)) return "range-last";
    if (isBetweenDays(day.dateObj, start, end)) return "range-middle";
    return "none";
  }

  // Waiting for second click — show hover preview
  if (end && isSameDay(start, end) && hoveredDate && isAfterDay(hoveredDate, start)) {
    if (isSameDay(day.dateObj, start)) return "range-first";
    if (isSameDay(day.dateObj, hoveredDate)) return "range-last";
    if (isBetweenDays(day.dateObj, start, hoveredDate)) return "range-middle";
    return "none";
  }

  // Single date selected (start only, no hover)
  if (isSameDay(day.dateObj, start)) return "single";
  return "none";
}

const Calendar = ({
  displayedYear,
  displayedMonth,
  selectionMode,
  selectedDate,
  selectedStartDate,
  selectedEndDate,
  hoveredDate,
  weekStartsOn,
  timeframe,
  isDateDisabled,
  onDayClick,
  onDayHover,
  onNavigateMonth,
  testId,
}: CalendarProps) => {
  const grid = useMemo(
    () => buildCalendarGrid(displayedYear, displayedMonth, weekStartsOn),
    [displayedYear, displayedMonth, weekStartsOn],
  );

  const dayNames = useMemo(() => getOrderedDayNames(weekStartsOn), [weekStartsOn]);

  const isDayDisabled = useCallback(
    (day: DayData): boolean => {
      if (!isDateInTimeframe(day.dateObj, timeframe)) return true;
      if (isDateDisabled?.(day.dateObj)) return true;
      return false;
    },
    [timeframe, isDateDisabled],
  );

  return (
    <div className="flex w-[280px] min-w-[280px] flex-col" data-testid={testId}>
      {/* Month/year header with navigation */}
      <div className="mb-2 flex items-center justify-between">
        <button
          type="button"
          className="flex h-8 w-8 cursor-pointer items-center justify-center rounded-full text-text-primary hover:bg-core-primary-5"
          onClick={() => onNavigateMonth(-1)}
          aria-label="Previous month"
          data-testid={testId ? `${testId}-prev-month` : undefined}
        >
          <ChevronDown className="rotate-90" width="w-3" />
        </button>
        <span className="text-emphasis-300 whitespace-nowrap text-text-primary">
          {MONTH_NAMES[displayedMonth]} {displayedYear}
        </span>
        <button
          type="button"
          className="flex h-8 w-8 cursor-pointer items-center justify-center rounded-full text-text-primary hover:bg-core-primary-5"
          onClick={() => onNavigateMonth(1)}
          aria-label="Next month"
          data-testid={testId ? `${testId}-next-month` : undefined}
        >
          <ChevronDown className="-rotate-90" width="w-3" />
        </button>
      </div>

      {/* Day name headers */}
      <div className="grid grid-cols-7">
        {dayNames.map((name) => (
          <div key={name} className="flex h-10 items-center justify-center text-200 text-text-primary-50">
            {name}
          </div>
        ))}
      </div>

      {/* Calendar grid */}
      <div className="grid grid-cols-7 gap-y-0.5">
        {grid.flat().map((day) => {
          const disabled = isDayDisabled(day);
          const selection = getDaySelection(
            day,
            selectionMode,
            selectedDate,
            selectedStartDate,
            selectedEndDate,
            hoveredDate,
          );
          const isSelected = selection !== "none";
          const isRangeEnd = selection === "range-first" || selection === "range-last" || selection === "single";

          return (
            <div
              key={`${day.year}-${day.month}-${day.date}`}
              className={clsx("flex h-10 items-center justify-center", {
                "bg-core-accent-10":
                  selection === "range-middle" || selection === "range-first" || selection === "range-last",
                "rounded-l-full": selection === "range-first",
                "rounded-r-full": selection === "range-last",
              })}
            >
              <button
                type="button"
                className={clsx("flex h-9 w-9 items-center justify-center rounded-full text-300 transition-colors", {
                  // Disabled
                  "cursor-not-allowed text-text-primary-30": disabled,
                  // Selected (single or range endpoints)
                  "bg-core-accent-fill text-text-base-contrast-static": isRangeEnd && !disabled,
                  // Range middle
                  "text-text-primary": selection === "range-middle" && !disabled,
                  // Today indicator (when not selected)
                  "ring-1 ring-border-20": day.today && !isSelected && !disabled,
                  // Normal interactive
                  "cursor-pointer text-text-primary hover:bg-core-primary-5":
                    !disabled && !isSelected && day.isCurrentMonth,
                  "cursor-pointer text-text-primary-30 hover:bg-core-primary-5":
                    !disabled && !isSelected && !day.isCurrentMonth,
                })}
                disabled={disabled}
                onClick={() => !disabled && onDayClick(day.dateObj)}
                onMouseEnter={() => !disabled && onDayHover(day.dateObj)}
                onMouseLeave={() => onDayHover(undefined)}
                tabIndex={disabled ? -1 : 0}
                aria-label={`${MONTH_NAMES[day.month]} ${day.date}, ${day.year}`}
                aria-disabled={disabled}
                aria-selected={isSelected}
                data-testid={testId && day.isCurrentMonth ? `${testId}-day-${day.date}` : undefined}
              >
                {day.date}
              </button>
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default Calendar;
