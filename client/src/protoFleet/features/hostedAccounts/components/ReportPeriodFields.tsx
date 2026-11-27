import {
  type CalendarRange,
  calendarRangeLabel,
  calendarRangeValue,
  calendarToday,
  type ReportingYearStart,
  type ReportPeriodOption,
  scheduledPeriodOptions,
} from "../reportPeriodOptions";
import { addDaysToDateValue, formatDateValue, parseDate } from "@/protoFleet/features/settings/utils/scheduleDateUtils";
import { DatePickerField } from "@/shared/components/DatePicker";
import Select from "@/shared/components/Select";

const periodLabels: Record<string, string> = {
  monthly: "Reporting month",
  quarterly: "Reporting quarter",
  annual: "Reporting year",
};

// Scheduled reports cover one whole month, quarter or reporting year, so the user picks the period
// (a year, then a month or quarter within it) rather than its boundaries. Ad hoc reports take a first and last day; every boundary is a
// midnight in the reporting timezone.
const ReportPeriodFields = ({
  kind,
  timezone,
  yearStart,
  quarterMonth,
  earliest,
  range,
  onChange,
}: {
  kind: string;
  timezone: string;
  yearStart: ReportingYearStart;
  quarterMonth: number;
  // Calendar day the list must reach back to, when that is further than its default.
  earliest?: string;
  range: Partial<CalendarRange>;
  onChange: (range: Partial<CalendarRange>) => void;
}) => {
  const today = calendarToday(timezone);
  if (kind === "ad_hoc") {
    const lastDay = range.end ? addDaysToDateValue(range.end, -1) : undefined;
    const day = (date: Date) => formatDateValue(date);
    return (
      <div className="grid gap-4 tablet:grid-cols-2">
        <DatePickerField
          id="period-first-day"
          label="First day"
          labelPlacement="floating"
          selectedDate={(range.start && parseDate(range.start)) || undefined}
          onSelectedDateChange={(date) => onChange({ ...range, start: day(date) })}
          isDateDisabled={(date) => day(date) >= today || (!!lastDay && day(date) > lastDay)}
          popoverRenderMode="portal-scrolling"
          testId="period-first-day"
        />
        <DatePickerField
          id="period-last-day"
          label="Last day"
          labelPlacement="floating"
          selectedDate={(lastDay && parseDate(lastDay)) || undefined}
          onSelectedDateChange={(date) => onChange({ ...range, end: addDaysToDateValue(day(date), 1) })}
          isDateDisabled={(date) => day(date) >= today || (!!range.start && day(date) < range.start)}
          popoverRenderMode="portal-scrolling"
          testId="period-last-day"
        />
      </div>
    );
  }
  // A period seeded from the work queue or an existing draft may be older than the offered list, or
  // not line up with it at all.
  const floor = range.start && (!earliest || range.start < earliest) ? range.start : earliest;
  const options = scheduledPeriodOptions(kind, today, yearStart, quarterMonth, floor);
  const value = calendarRangeValue(range);
  if (range.start && range.end && !options.some((option) => option.value === value)) {
    const label = calendarRangeLabel(range as CalendarRange);
    options.push({
      start: range.start,
      end: range.end,
      value,
      label,
      year: range.start.slice(0, 4),
      periodLabel: label,
    });
    options.sort((a, b) => (a.start < b.start ? 1 : -1));
  }
  const selected = options.find((option) => option.value === value);
  const pick = (option?: ReportPeriodOption) => option && onChange({ start: option.start, end: option.end });
  const period = (periods: ReportPeriodOption[]) => (
    <Select
      id="report-period"
      label={periodLabels[kind] ?? "Reporting period"}
      value={selected?.value ?? ""}
      options={periods.map((option) => ({ value: option.value, label: option.periodLabel }))}
      onChange={(next) => pick(options.find((option) => option.value === next))}
    />
  );
  if (kind === "annual") return period(options);
  const years = [...new Set(options.map((option) => option.year))];
  const year = selected?.year ?? years[0] ?? "";
  return (
    <div className="grid gap-4 tablet:grid-cols-2">
      <Select
        id="report-period-year"
        label="Year"
        value={year}
        options={years.map((value) => ({ value, label: value }))}
        onChange={(next) => {
          // Keep the same month or quarter when that year has it, otherwise take its latest period.
          const periods = options.filter((option) => option.year === next);
          pick(periods.find((option) => option.start.slice(5) === selected?.start.slice(5)) ?? periods[0]);
        }}
      />
      {period(options.filter((option) => option.year === year))}
    </div>
  );
};

export default ReportPeriodFields;
