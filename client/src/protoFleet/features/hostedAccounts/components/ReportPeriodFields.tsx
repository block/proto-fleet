import {
  type CalendarRange,
  calendarRangeLabel,
  calendarRangeValue,
  calendarToday,
  type ReportingYearStart,
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
// rather than its boundaries. Ad hoc reports take a first and last day; every boundary is a
// midnight in the reporting timezone.
const ReportPeriodFields = ({
  kind,
  timezone,
  yearStart,
  quarterMonth,
  range,
  onChange,
}: {
  kind: string;
  timezone: string;
  yearStart: ReportingYearStart;
  quarterMonth: number;
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
  const options = scheduledPeriodOptions(kind, today, yearStart, quarterMonth);
  const value = calendarRangeValue(range);
  // A period seeded from the work queue or an existing draft may fall outside the offered list.
  if (range.start && range.end && !options.some((option) => option.value === value))
    options.unshift({ start: range.start, end: range.end, value, label: calendarRangeLabel(range as CalendarRange) });
  return (
    <Select
      id="report-period"
      label={periodLabels[kind] ?? "Reporting period"}
      value={range.start && range.end ? value : ""}
      options={options}
      onChange={(next) => {
        const option = options.find((candidate) => candidate.value === next);
        if (option) onChange({ start: option.start, end: option.end });
      }}
    />
  );
};

export default ReportPeriodFields;
