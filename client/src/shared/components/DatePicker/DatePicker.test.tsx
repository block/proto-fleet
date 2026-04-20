import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import {
  buildCalendarGrid,
  computePresetDates,
  formatDate,
  formatDisplayDate,
  formatDisplayDateTime,
  getEndOfMonth,
  getEndOfWeek,
  getStartOfMonth,
  getStartOfWeek,
  isAfterDay,
  isBeforeDay,
  isBetweenDays,
  isDateRangeSelectable,
  isSameDay,
  parseDate,
  parseDateTime,
} from "./utils";
import DatePicker from ".";

// ── Utility tests ──

describe("Date utilities", () => {
  test("isSameDay returns true for same dates", () => {
    const a = new Date(2026, 0, 15);
    const b = new Date(2026, 0, 15, 14, 30);
    expect(isSameDay(a, b)).toBe(true);
  });

  test("isSameDay returns false for different dates", () => {
    expect(isSameDay(new Date(2026, 0, 15), new Date(2026, 0, 16))).toBe(false);
  });

  test("isBeforeDay and isAfterDay", () => {
    const jan15 = new Date(2026, 0, 15);
    const jan16 = new Date(2026, 0, 16);
    expect(isBeforeDay(jan15, jan16)).toBe(true);
    expect(isAfterDay(jan16, jan15)).toBe(true);
    expect(isBeforeDay(jan15, jan15)).toBe(false);
  });

  test("isBetweenDays", () => {
    const jan10 = new Date(2026, 0, 10);
    const jan15 = new Date(2026, 0, 15);
    const jan20 = new Date(2026, 0, 20);
    expect(isBetweenDays(jan15, jan10, jan20)).toBe(true);
    expect(isBetweenDays(jan10, jan10, jan20)).toBe(false);
    expect(isBetweenDays(jan20, jan10, jan20)).toBe(false);
  });

  test("getStartOfWeek and getEndOfWeek (Sunday start)", () => {
    const wed = new Date(2026, 0, 14); // Wednesday
    const start = getStartOfWeek(wed, 0);
    const end = getEndOfWeek(wed, 0);
    expect(start.getDay()).toBe(0); // Sunday
    expect(end.getDay()).toBe(6); // Saturday
  });

  test("getStartOfWeek (Monday start)", () => {
    const wed = new Date(2026, 0, 14); // Wednesday
    const start = getStartOfWeek(wed, 1);
    expect(start.getDay()).toBe(1); // Monday
  });

  test("getStartOfMonth and getEndOfMonth", () => {
    const date = new Date(2026, 1, 15); // Feb 15
    expect(getStartOfMonth(date).getDate()).toBe(1);
    expect(getEndOfMonth(date).getDate()).toBe(28); // 2026 is not a leap year
  });

  test("formatDate produces YYYY-MM-DD", () => {
    expect(formatDate(new Date(2026, 0, 5))).toBe("2026-01-05");
  });

  test("formatDisplayDate produces readable format", () => {
    expect(formatDisplayDate(new Date(2026, 0, 15))).toBe("Jan 15, 2026");
  });

  test("parseDate parses valid date string", () => {
    const date = parseDate("2026-03-15");
    expect(date).not.toBeNull();
    expect(date!.getFullYear()).toBe(2026);
    expect(date!.getMonth()).toBe(2);
    expect(date!.getDate()).toBe(15);
  });

  test("parseDate returns null for invalid string", () => {
    expect(parseDate("not-a-date")).toBeNull();
    expect(parseDate("2026/01/01")).toBeNull();
  });

  test("parseDate rejects overflowed calendar dates", () => {
    expect(parseDate("2026-02-31")).toBeNull();
    expect(parseDate("2026-13-01")).toBeNull();
  });

  test("parseDateTime rejects overflowed calendar and clock values", () => {
    expect(parseDateTime("2026-02-31 10:00")).toBeNull();
    expect(parseDateTime("2026-01-01 24:61")).toBeNull();
  });

  test("isDateRangeSelectable rejects disabled interior days", () => {
    const isSelectableDate = (date: Date) => date.getDate() !== 12;

    expect(isDateRangeSelectable(new Date(2026, 0, 10), new Date(2026, 0, 15), isSelectableDate)).toBe(false);
    expect(isDateRangeSelectable(new Date(2026, 0, 10), new Date(2026, 0, 11), isSelectableDate)).toBe(true);
  });
});

describe("buildCalendarGrid", () => {
  test("generates 6 rows of 7 days", () => {
    const grid = buildCalendarGrid(2026, 0, 0); // January 2026, Sunday start
    expect(grid).toHaveLength(6);
    grid.forEach((week) => expect(week).toHaveLength(7));
  });

  test("marks current month days correctly", () => {
    const grid = buildCalendarGrid(2026, 0, 0); // January 2026
    const allDays = grid.flat();
    const janDays = allDays.filter((d) => d.isCurrentMonth);
    expect(janDays).toHaveLength(31);
  });

  test("first cell is correct for Monday start", () => {
    const grid = buildCalendarGrid(2026, 0, 1); // January 2026, Monday start
    // Jan 1, 2026 is a Thursday, so Monday start means Dec 29 is first cell
    expect(grid[0][0].isCurrentMonth).toBe(false);
    expect(grid[0][0].date).toBe(29);
  });
});

describe("computePresetDates", () => {
  test("today returns today", () => {
    const result = computePresetDates("today", 0);
    expect(result).not.toBeNull();
    expect(isSameDay(result!.startDate, new Date())).toBe(true);
  });

  test("yesterday returns yesterday", () => {
    const result = computePresetDates("yesterday", 0);
    const yesterday = new Date();
    yesterday.setDate(yesterday.getDate() - 1);
    expect(result).not.toBeNull();
    expect(isSameDay(result!.startDate, yesterday)).toBe(true);
  });

  test("custom returns null", () => {
    expect(computePresetDates("custom", 0)).toBeNull();
  });
});

// ── Component tests ──

describe("DatePicker", () => {
  test("renders trigger button", () => {
    render(<DatePicker testId="dp" />);
    expect(screen.getByTestId("dp-trigger")).toBeDefined();
  });

  test("shows placeholder text when no date selected", () => {
    render(<DatePicker testId="dp" />);
    expect(screen.getByText("Select date")).toBeDefined();
  });

  test("shows range placeholder in range mode", () => {
    render(<DatePicker selectionMode="range" testId="dp" />);
    expect(screen.getByText("Select dates")).toBeDefined();
  });

  test("renders a floating label without the placeholder text when closed", () => {
    render(<DatePicker label="Start date" floatingLabel testId="dp" />);

    expect(screen.getByText("Start date")).toBeDefined();
    expect(screen.queryByText("Select date")).toBeNull();
  });

  test("renders the selected value with a floating label", () => {
    render(<DatePicker label="Start date" floatingLabel selectedDate={new Date(2026, 3, 8)} testId="dp" />);

    expect(screen.getByText("Start date")).toBeDefined();
    expect(screen.getByText("Apr 8, 2026")).toBeDefined();
  });

  test("keeps the floating-label value in the trigger's accessible name", () => {
    render(<DatePicker label="Start date" floatingLabel selectedDate={new Date(2026, 3, 8)} testId="dp" />);

    expect(screen.getByRole("button", { name: /start date.*apr 8, 2026/i })).toBeDefined();
  });

  test("opens calendar panel on click", () => {
    render(<DatePicker testId="dp" />);
    fireEvent.click(screen.getByTestId("dp-trigger"));
    expect(screen.getByTestId("dp-panel")).toBeDefined();
  });

  test("does not open when disabled", () => {
    render(<DatePicker disabled testId="dp" />);
    fireEvent.click(screen.getByTestId("dp-trigger"));
    expect(screen.queryByTestId("dp-panel")).toBeNull();
  });

  test("closes the panel when disabled turns on and keeps it closed after re-enable", () => {
    const { rerender } = render(<DatePicker testId="dp" />);

    fireEvent.click(screen.getByTestId("dp-trigger"));
    expect(screen.getByTestId("dp-panel")).toBeDefined();

    rerender(<DatePicker disabled testId="dp" />);
    expect(screen.queryByTestId("dp-panel")).toBeNull();

    rerender(<DatePicker testId="dp" />);
    expect(screen.queryByTestId("dp-panel")).toBeNull();
  });

  test("selects a date in single mode", () => {
    const onChange = vi.fn();
    render(<DatePicker selectionMode="single" onSelectedDateChange={onChange} testId="dp" />);
    fireEvent.click(screen.getByTestId("dp-trigger"));
    fireEvent.click(screen.getByTestId("dp-calendar-day-15"));
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange.mock.calls[0][0].getDate()).toBe(15);
  });

  test("renders preset menu when displayMenu is true", () => {
    render(<DatePicker displayMenu testId="dp" />);
    fireEvent.click(screen.getByTestId("dp-trigger"));
    expect(screen.getByTestId("dp-presets")).toBeDefined();
    expect(screen.getByText("Today")).toBeDefined();
  });

  test("does not render preset menu when displayMenu is false", () => {
    render(<DatePicker testId="dp" />);
    fireEvent.click(screen.getByTestId("dp-trigger"));
    expect(screen.queryByTestId("dp-presets")).toBeNull();
  });

  test("renders date inputs when withInputs is set", () => {
    render(<DatePicker withInputs="date" testId="dp" />);
    fireEvent.click(screen.getByTestId("dp-trigger"));
    expect(screen.getByTestId("dp-inputs")).toBeDefined();
  });

  test("can render the calendar panel in a portal for scrolling containers", () => {
    render(<DatePicker popoverRenderMode="portal-scrolling" testId="dp" />);

    fireEvent.click(screen.getByTestId("dp-trigger"));

    const panel = screen.getByTestId("dp-panel");
    expect(panel).toBeDefined();
    expect(screen.getByTestId("dp").contains(panel)).toBe(false);
  });

  test("moves focus into portal-scrolling panels when opened from the trigger", async () => {
    render(<DatePicker popoverRenderMode="portal-scrolling" testId="dp" />);

    fireEvent.click(screen.getByTestId("dp-trigger"));

    await waitFor(() => {
      expect(screen.getByLabelText("Previous month")).toHaveFocus();
    });
  });

  test("closes the calendar panel when focus leaves the picker", async () => {
    render(
      <>
        <DatePicker popoverRenderMode="portal-scrolling" testId="dp" />
        <button type="button">Outside</button>
      </>,
    );

    fireEvent.click(screen.getByTestId("dp-trigger"));
    expect(screen.getByTestId("dp-panel")).toBeDefined();

    fireEvent.focusIn(screen.getByLabelText("Previous month"));
    expect(screen.getByTestId("dp-panel")).toBeDefined();

    fireEvent.focusIn(screen.getByRole("button", { name: "Outside" }));

    await waitFor(() => {
      expect(screen.queryByTestId("dp-panel")).toBeNull();
    });
  });

  test("repositions portal-scrolling panels when a scroll container moves", async () => {
    let triggerTop = 100;

    render(
      <div data-testid="scroll-container" style={{ overflow: "auto", maxHeight: "200px" }}>
        <DatePicker popoverRenderMode="portal-scrolling" testId="dp" />
      </div>,
    );

    const triggerContainer = screen.getByTestId("dp-trigger").parentElement as HTMLDivElement;
    Object.defineProperty(triggerContainer, "getBoundingClientRect", {
      configurable: true,
      value: () => ({
        x: 40,
        y: triggerTop,
        top: triggerTop,
        left: 40,
        bottom: triggerTop + 56,
        right: 240,
        width: 200,
        height: 56,
        toJSON: () => ({}),
      }),
    });

    fireEvent.click(screen.getByTestId("dp-trigger"));

    const panel = screen.getByTestId("dp-panel");

    await waitFor(() => {
      expect(panel.style.top).toBe("164px");
    });

    triggerTop = 40;
    fireEvent.scroll(screen.getByTestId("scroll-container"));

    await waitFor(() => {
      expect(panel.style.top).toBe("104px");
    });
  });

  test("month navigation works", () => {
    render(<DatePicker testId="dp" />);
    fireEvent.click(screen.getByTestId("dp-trigger"));

    const monthLabel = screen.getByTestId("dp-panel").querySelector(".text-emphasis-300");
    const initialText = monthLabel?.textContent;

    fireEvent.click(screen.getByTestId("dp-calendar-next-month"));
    expect(monthLabel?.textContent).not.toBe(initialText);
  });

  test("renders adjacent month days as selectable muted dates", () => {
    const onChange = vi.fn();
    render(<DatePicker selectedDate={new Date(2026, 0, 15)} onSelectedDateChange={onChange} testId="dp" />);

    fireEvent.click(screen.getByTestId("dp-trigger"));

    const adjacentDay = screen.getByLabelText("February 1, 2026");
    expect(adjacentDay).toBeDefined();
    expect(adjacentDay).toHaveClass("text-text-primary-30");
    expect(adjacentDay).not.toHaveClass("invisible");
    expect(adjacentDay).not.toHaveAttribute("disabled");

    fireEvent.click(adjacentDay);

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange.mock.calls[0][0].getFullYear()).toBe(2026);
    expect(onChange.mock.calls[0][0].getMonth()).toBe(1);
    expect(onChange.mock.calls[0][0].getDate()).toBe(1);
  });

  test("disabled dates cannot be clicked", () => {
    const onChange = vi.fn();
    const isDateDisabled = (d: Date) => d.getDate() === 10;
    render(
      <DatePicker selectionMode="single" onSelectedDateChange={onChange} isDateDisabled={isDateDisabled} testId="dp" />,
    );
    fireEvent.click(screen.getByTestId("dp-trigger"));
    fireEvent.click(screen.getByTestId("dp-calendar-day-10"));
    expect(onChange).not.toHaveBeenCalled();
  });

  test("applies preset on click", () => {
    const onChange = vi.fn();
    render(<DatePicker selectionMode="single" displayMenu onSelectedDateChange={onChange} testId="dp" />);
    fireEvent.click(screen.getByTestId("dp-trigger"));
    fireEvent.click(screen.getByTestId("dp-presets-today"));
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(isSameDay(onChange.mock.calls[0][0], new Date())).toBe(true);
  });

  test("range selection requires two clicks", () => {
    const onRangeChange = vi.fn();
    render(<DatePicker selectionMode="range" onSelectedDateRangeChange={onRangeChange} testId="dp" />);
    fireEvent.click(screen.getByTestId("dp-trigger"));

    // First click: start date
    fireEvent.click(screen.getByTestId("dp-calendar-day-10"));
    expect(onRangeChange).not.toHaveBeenCalled();

    // Second click: end date
    fireEvent.click(screen.getByTestId("dp-calendar-day-20"));
    expect(onRangeChange).toHaveBeenCalledTimes(1);
    expect(onRangeChange.mock.calls[0][0].getDate()).toBe(10);
    expect(onRangeChange.mock.calls[0][1].getDate()).toBe(20);
  });

  test("allows selecting a one-day range by clicking the same day twice", () => {
    const onRangeChange = vi.fn();
    render(<DatePicker selectionMode="range" onSelectedDateRangeChange={onRangeChange} testId="dp" />);

    fireEvent.click(screen.getByTestId("dp-trigger"));
    fireEvent.click(screen.getByTestId("dp-calendar-day-10"));
    fireEvent.click(screen.getByTestId("dp-calendar-day-10"));

    expect(onRangeChange).toHaveBeenCalledTimes(1);
    expect(onRangeChange.mock.calls[0][0].getDate()).toBe(10);
    expect(onRangeChange.mock.calls[0][1].getDate()).toBe(10);
  });

  test("uses the pending start date when completing a controlled range", () => {
    const onRangeChange = vi.fn();
    render(
      <DatePicker
        selectionMode="range"
        selectedStartDate={new Date(2026, 0, 5)}
        selectedEndDate={new Date(2026, 0, 7)}
        onSelectedDateRangeChange={onRangeChange}
        testId="dp"
      />,
    );

    fireEvent.click(screen.getByTestId("dp-trigger"));
    fireEvent.click(screen.getByTestId("dp-calendar-day-10"));
    fireEvent.click(screen.getByTestId("dp-calendar-day-15"));

    expect(onRangeChange).toHaveBeenCalledTimes(1);
    expect(onRangeChange.mock.calls[0][0].getDate()).toBe(10);
    expect(onRangeChange.mock.calls[0][1].getDate()).toBe(15);
  });

  test("uses the pending start date when finishing a controlled range through inputs", () => {
    const onRangeChange = vi.fn();
    render(
      <DatePicker
        selectionMode="range"
        withInputs="date"
        selectedStartDate={new Date(2026, 0, 5)}
        selectedEndDate={new Date(2026, 0, 7)}
        onSelectedDateRangeChange={onRangeChange}
        testId="dp"
      />,
    );

    fireEvent.click(screen.getByTestId("dp-trigger"));
    fireEvent.click(screen.getByTestId("dp-calendar-day-10"));
    fireEvent.change(screen.getByTestId("dp-inputs-end-input"), { target: { value: "2026-01-15" } });
    fireEvent.blur(screen.getByTestId("dp-inputs-end-input"));

    expect(onRangeChange).toHaveBeenCalledTimes(1);
    expect(onRangeChange.mock.calls[0][0].getDate()).toBe(10);
    expect(onRangeChange.mock.calls[0][1].getDate()).toBe(15);
  });

  test("allows manual range input to create a new range from empty state", () => {
    const onRangeChange = vi.fn();
    render(
      <DatePicker selectionMode="range" withInputs="date" onSelectedDateRangeChange={onRangeChange} testId="dp" />,
    );

    fireEvent.click(screen.getByTestId("dp-trigger"));
    fireEvent.change(screen.getByTestId("dp-inputs-start-input"), { target: { value: "2026-01-10" } });
    fireEvent.change(screen.getByTestId("dp-inputs-end-input"), { target: { value: "2026-01-15" } });
    fireEvent.blur(screen.getByTestId("dp-inputs-end-input"));

    expect(onRangeChange).toHaveBeenCalledTimes(1);
    expect(onRangeChange.mock.calls[0][0].getDate()).toBe(10);
    expect(onRangeChange.mock.calls[0][1].getDate()).toBe(15);
  });

  test("rejects inverted manual ranges", () => {
    const onRangeChange = vi.fn();
    render(
      <DatePicker selectionMode="range" withInputs="date" onSelectedDateRangeChange={onRangeChange} testId="dp" />,
    );

    fireEvent.click(screen.getByTestId("dp-trigger"));
    fireEvent.change(screen.getByTestId("dp-inputs-start-input"), { target: { value: "2026-01-20" } });
    fireEvent.change(screen.getByTestId("dp-inputs-end-input"), { target: { value: "2026-01-15" } });
    fireEvent.blur(screen.getByTestId("dp-inputs-end-input"));

    expect(onRangeChange).not.toHaveBeenCalled();
  });

  test("ignores typed dates that fall outside the timeframe", () => {
    const onChange = vi.fn();
    const yesterday = new Date();
    yesterday.setDate(yesterday.getDate() - 1);

    render(
      <DatePicker
        selectionMode="single"
        withInputs="date"
        timeframe="future"
        onSelectedDateChange={onChange}
        testId="dp"
      />,
    );

    fireEvent.click(screen.getByTestId("dp-trigger"));
    fireEvent.change(screen.getByTestId("dp-inputs-date-input"), { target: { value: formatDate(yesterday) } });
    fireEvent.blur(screen.getByTestId("dp-inputs-date-input"));

    expect(onChange).not.toHaveBeenCalled();
  });

  test("clears the displayed value when a controlled date is reset", () => {
    const { rerender } = render(<DatePicker selectedDate={new Date(2026, 0, 15)} testId="dp" />);

    expect(screen.getByText("Jan 15, 2026")).toBeDefined();

    rerender(<DatePicker selectedDate={undefined} testId="dp" />);

    expect(screen.getByText("Select date")).toBeDefined();
  });

  test("hides built-in range presets that do not fit the future timeframe", () => {
    render(<DatePicker selectionMode="range" displayMenu timeframe="future" testId="dp" />);

    fireEvent.click(screen.getByTestId("dp-trigger"));

    expect(screen.getByText("Today")).toBeDefined();
    expect(screen.queryByText("This Week")).toBeNull();
    expect(screen.queryByText("This Month")).toBeNull();
  });

  test("resets a pending range when the picker closes", () => {
    const onRangeChange = vi.fn();
    render(<DatePicker selectionMode="range" onSelectedDateRangeChange={onRangeChange} testId="dp" />);

    fireEvent.click(screen.getByTestId("dp-trigger"));
    fireEvent.click(screen.getByTestId("dp-calendar-day-10"));
    fireEvent.click(screen.getByTestId("dp-trigger"));

    fireEvent.click(screen.getByTestId("dp-trigger"));
    fireEvent.click(screen.getByTestId("dp-calendar-day-15"));
    expect(onRangeChange).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("dp-calendar-day-20"));
    expect(onRangeChange).toHaveBeenCalledTimes(1);
    expect(onRangeChange.mock.calls[0][0].getDate()).toBe(15);
    expect(onRangeChange.mock.calls[0][1].getDate()).toBe(20);
  });

  test("syncs the visible month when a controlled selection changes", () => {
    const { rerender } = render(<DatePicker selectedDate={new Date(2026, 0, 15)} testId="dp" />);

    fireEvent.click(screen.getByTestId("dp-trigger"));
    expect(screen.getByText("January 2026")).toBeDefined();

    rerender(<DatePicker selectedDate={new Date(2026, 2, 15)} testId="dp" />);

    expect(screen.getByText("March 2026")).toBeDefined();
  });

  test("shows times in the trigger for date-time single selections", () => {
    render(<DatePicker selectedDate={new Date(2026, 0, 10, 8, 30)} withInputs="date-and-time" testId="dp" />);

    expect(screen.getByText(formatDisplayDateTime(new Date(2026, 0, 10, 8, 30)))).toBeDefined();
  });

  test("keeps same-day date-time ranges expanded in the trigger", () => {
    render(
      <DatePicker
        selectionMode="range"
        withInputs="date-and-time"
        selectedStartDate={new Date(2026, 0, 10, 8, 0)}
        selectedEndDate={new Date(2026, 0, 10, 20, 0)}
        testId="dp"
      />,
    );

    expect(
      screen.getByText(
        `${formatDisplayDateTime(new Date(2026, 0, 10, 8, 0))} — ${formatDisplayDateTime(new Date(2026, 0, 10, 20, 0))}`,
      ),
    ).toBeDefined();
  });

  test("rejects manual ranges that span disabled interior days", () => {
    const onRangeChange = vi.fn();
    const isDateDisabled = (date: Date) => date.getDate() === 12;

    render(
      <DatePicker
        selectionMode="range"
        withInputs="date"
        isDateDisabled={isDateDisabled}
        onSelectedDateRangeChange={onRangeChange}
        testId="dp"
      />,
    );

    fireEvent.click(screen.getByTestId("dp-trigger"));
    fireEvent.change(screen.getByTestId("dp-inputs-start-input"), { target: { value: "2026-01-10" } });
    fireEvent.change(screen.getByTestId("dp-inputs-end-input"), { target: { value: "2026-01-15" } });
    fireEvent.blur(screen.getByTestId("dp-inputs-end-input"));

    expect(onRangeChange).not.toHaveBeenCalled();
  });
});
