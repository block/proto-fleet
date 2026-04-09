import { useMemo } from "react";

import { getFutureScheduleRuns } from "@/protoFleet/features/settings/components/Schedules/scheduleRunUtils";
import {
  describeSelectedTargets,
  type ScheduleFormValues,
  validateSchedule,
  weekdayOptions,
} from "@/protoFleet/features/settings/components/Schedules/scheduleValidation";
import {
  addDaysToDateValue,
  buildDateInTimeZone,
  parseDate,
} from "@/protoFleet/features/settings/utils/scheduleDateUtils";

type PreviewFormatters = {
  date: Intl.DateTimeFormat;
  longDate: Intl.DateTimeFormat;
  time: Intl.DateTimeFormat;
  weekdayDate: Intl.DateTimeFormat;
};

const createPreviewFormatters = (timeZone: string): PreviewFormatters => ({
  date: new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone,
  }),
  longDate: new Intl.DateTimeFormat(undefined, {
    month: "long",
    day: "numeric",
    year: "numeric",
    timeZone,
  }),
  time: new Intl.DateTimeFormat(undefined, {
    hour: "numeric",
    minute: "2-digit",
    timeZone,
  }),
  weekdayDate: new Intl.DateTimeFormat(undefined, {
    weekday: "long",
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone,
  }),
});

const weekdayLabelByValue = weekdayOptions.reduce<Record<number, string>>((result, option) => {
  result[option.value] = option.label;
  return result;
}, {});

const formatDateAtTime = (date: Date, formatters: PreviewFormatters, formatter = formatters.longDate) =>
  `${formatter.format(date)} at ${formatters.time.format(date)}`;

const buildCalendarDateInTimeZone = (dateValue: string, timeZone: string) =>
  buildDateInTimeZone(dateValue, "12:00", timeZone) ?? parseDate(dateValue);

const formatOrdinal = (value: number) => {
  const suffix =
    value % 10 === 1 && value % 100 !== 11
      ? "st"
      : value % 10 === 2 && value % 100 !== 12
        ? "nd"
        : value % 10 === 3 && value % 100 !== 13
          ? "rd"
          : "th";

  return `${value}${suffix}`;
};

const getTargetPhrase = (values: ScheduleFormValues) => {
  const summary = describeSelectedTargets(values);

  if (summary === "Applies to all miners") {
    return "for all miners";
  }

  return summary.replace("Applies to ", "for ");
};

const getActionPhrase = (values: ScheduleFormValues) => {
  if (values.action === "reboot") {
    return "Reboot";
  }

  if (values.action === "sleep") {
    return "Put miners to sleep";
  }

  return `Set power to ${values.powerTargetMode === "max" ? "max" : "default"}`;
};

const getRecurringSummary = (values: ScheduleFormValues) => {
  if (values.frequency === "daily") {
    return "Every day";
  }

  if (values.frequency === "weekly") {
    const uniqueDays = Array.from(new Set(values.daysOfWeek)).sort((left, right) => left - right);

    if (uniqueDays.length === 7) {
      return "Every day";
    }

    const days = uniqueDays.map((day) => weekdayLabelByValue[day]).filter(Boolean);

    return days.join(", ");
  }

  return `${formatOrdinal(Number(values.dayOfMonth) || 1)} day of month`;
};

const getRecurringSentenceFragment = (values: ScheduleFormValues) => {
  const recurringSummary = getRecurringSummary(values);

  if (recurringSummary === "Every day") {
    return "every day";
  }

  if (values.frequency === "monthly") {
    return `on the ${recurringSummary}`;
  }

  return `on ${recurringSummary}`;
};

const formatTimeWindow = (values: ScheduleFormValues, dateValue: string, formatters: PreviewFormatters) => {
  const start = buildDateInTimeZone(dateValue, values.startTime, values.timezone);

  if (!start) {
    return values.startTime;
  }

  if (values.scheduleType !== "recurring" || values.action !== "setPowerTarget") {
    return formatters.time.format(start);
  }

  const endDateValue = values.endTime < values.startTime ? addDaysToDateValue(dateValue, 1) : dateValue;
  const end = buildDateInTimeZone(endDateValue, values.endTime, values.timezone);

  if (!end) {
    return formatters.time.format(start);
  }

  return `${formatters.time.format(start)} - ${formatters.time.format(end)}`;
};

const getSchedulePreviewSummary = (values: ScheduleFormValues, formatters: PreviewFormatters) => {
  const actionPhrase = getActionPhrase(values);
  const targetPhrase = getTargetPhrase(values);

  if (values.scheduleType === "oneTime") {
    const start = buildDateInTimeZone(values.startDate, values.startTime, values.timezone);

    return start
      ? `${actionPhrase} ${targetPhrase} on ${formatDateAtTime(start, formatters)}.`
      : `${actionPhrase} ${targetPhrase} on ${values.startDate} at ${values.startTime}.`;
  }

  const summaryParts = [
    `${actionPhrase} ${targetPhrase}`,
    getRecurringSentenceFragment(values),
    values.action === "setPowerTarget"
      ? `from ${formatTimeWindow(values, values.startDate, formatters)}`
      : `at ${formatTimeWindow(values, values.startDate, formatters)}`,
  ];
  const startDate = buildCalendarDateInTimeZone(values.startDate, values.timezone);

  if (startDate) {
    summaryParts.push(`starting ${formatters.date.format(startDate)}`);
  }

  if (values.endBehavior === "endDate") {
    const endDate = buildCalendarDateInTimeZone(values.endDate, values.timezone);

    if (endDate) {
      summaryParts.push(`ending ${formatters.date.format(endDate)}`);
    }
  }

  return `${summaryParts.join(" ")}.`;
};

interface SchedulePreviewProps {
  values: ScheduleFormValues;
  isEditMode?: boolean;
}

const SchedulePreview = ({ values, isEditMode = false }: SchedulePreviewProps) => {
  const formatters = useMemo(() => createPreviewFormatters(values.timezone), [values.timezone]);
  const errors = useMemo(() => validateSchedule(values), [values]);
  const previewSummary = useMemo(() => getSchedulePreviewSummary(values, formatters), [formatters, values]);
  const previewRuns = useMemo(() => getFutureScheduleRuns(values), [values]);
  const isReady = Object.entries(errors).every(([field]) => field === "name");
  const mobilePreviewRun = previewRuns[0];

  return (
    <>
      <div className="flex min-h-16 items-center justify-center px-6 py-4 laptop:hidden desktop:hidden">
        {!isReady ? (
          <div className="text-300 text-text-primary-50">Complete the schedule fields to preview the run.</div>
        ) : mobilePreviewRun ? (
          <div className="w-full text-center">
            <div className="text-emphasis-300 text-text-primary">{previewSummary}</div>
          </div>
        ) : (
          <div className="text-300 text-text-primary-50">No preview available</div>
        )}
      </div>

      <div className="hidden flex-col justify-center px-16 pt-6 pb-4 laptop:flex laptop:flex-1 desktop:flex desktop:flex-1">
        {!isReady ? (
          <div className="max-w-[420px] text-300 text-text-primary-70">
            Complete the date, time, and targeting fields to preview the schedule before saving.
          </div>
        ) : (
          <div className="max-w-[520px]">
            <div className={isEditMode ? "text-heading-100 text-text-primary" : "text-heading-200 text-text-primary"}>
              {previewSummary}
            </div>

            <div className="mt-10 text-emphasis-300 text-text-primary-50">Upcoming runs</div>
            {previewRuns.length === 0 ? (
              <div className="mt-5 text-300 text-text-primary-70">No future runs match the current schedule.</div>
            ) : (
              <div className="mt-5 flex flex-col gap-4">
                {previewRuns.map((run, index) => (
                  <div key={`${run.start.toISOString()}-${index}`} className="text-heading-100 text-text-primary">
                    {run.end
                      ? `${formatDateAtTime(run.start, formatters, formatters.weekdayDate)} - ${formatters.time.format(run.end)}`
                      : formatDateAtTime(run.start, formatters, formatters.weekdayDate)}
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </>
  );
};

export default SchedulePreview;
