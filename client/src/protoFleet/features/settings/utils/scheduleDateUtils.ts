type DateParts = {
  year: number;
  month: number;
  day: number;
};

type TimeParts = {
  hours: number;
  minutes: number;
};

export const parseDateParts = (value: string): DateParts | null => {
  const [year, month, day] = value.split("-").map(Number);

  if (!year || !month || !day) {
    return null;
  }

  return { year, month, day };
};

export const parseTimeParts = (value: string): TimeParts | null => {
  const [hours, minutes] = value.split(":").map(Number);

  if (Number.isNaN(hours) || Number.isNaN(minutes)) {
    return null;
  }

  return { hours, minutes };
};

export const parseDate = (value: string) => {
  const parts = parseDateParts(value);

  if (!parts) {
    return null;
  }

  const date = new Date(parts.year, parts.month - 1, parts.day);
  return formatDateValue(date) === value ? date : null;
};

export const parseTime = (value: string) => {
  const parts = parseTimeParts(value);

  if (!parts) {
    return null;
  }

  const date = new Date();
  date.setHours(parts.hours, parts.minutes, 0, 0);
  return date;
};

export const parseDateTime = (dateValue: string, timeValue: string) => {
  const date = parseDate(dateValue);
  const time = parseTime(timeValue);

  if (!date || !time) {
    return null;
  }

  date.setHours(time.getHours(), time.getMinutes(), 0, 0);
  return date;
};

export const formatDateParts = (parts: DateParts) =>
  `${parts.year}-${String(parts.month).padStart(2, "0")}-${String(parts.day).padStart(2, "0")}`;

export const formatDateValue = (date: Date) =>
  formatDateParts({
    year: date.getFullYear(),
    month: date.getMonth() + 1,
    day: date.getDate(),
  });

export const formatTimeZoneDateParts = (parts: DateParts) =>
  formatDateParts({
    year: parts.year,
    month: parts.month,
    day: parts.day,
  });

export const getTimeZoneDateTimeParts = (date: Date, timeZone: string) => {
  try {
    const parts = new Intl.DateTimeFormat("en-CA", {
      timeZone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
    }).formatToParts(date);
    const getNumericPart = (type: Intl.DateTimeFormatPartTypes) =>
      Number(parts.find((part) => part.type === type)?.value);

    return {
      year: getNumericPart("year"),
      month: getNumericPart("month"),
      day: getNumericPart("day"),
      hours: getNumericPart("hour"),
      minutes: getNumericPart("minute"),
    };
  } catch {
    return null;
  }
};

export const addDaysToDateValue = (dateValue: string, days: number) => {
  const parsed = parseDate(dateValue);

  if (!parsed) {
    return dateValue;
  }

  parsed.setDate(parsed.getDate() + days);

  return formatDateValue(parsed);
};

export const buildDateInTimeZone = (dateValue: string, timeValue: string, timeZone: string) => {
  const dateParts = parseDateParts(dateValue);
  const timeParts = parseTimeParts(timeValue);

  if (!dateParts || !timeParts) {
    return null;
  }

  const desiredUtcTime = Date.UTC(
    dateParts.year,
    dateParts.month - 1,
    dateParts.day,
    timeParts.hours,
    timeParts.minutes,
  );
  const matchesRequestedParts = (candidateParts: ReturnType<typeof getTimeZoneDateTimeParts>) => {
    if (!candidateParts) {
      return false;
    }

    return (
      candidateParts.year === dateParts.year &&
      candidateParts.month === dateParts.month &&
      candidateParts.day === dateParts.day &&
      candidateParts.hours === timeParts.hours &&
      candidateParts.minutes === timeParts.minutes
    );
  };
  let candidate = new Date(desiredUtcTime);

  for (let attempt = 0; attempt < 2; attempt += 1) {
    const candidateParts = getTimeZoneDateTimeParts(candidate, timeZone);

    if (!candidateParts) {
      return null;
    }

    if (matchesRequestedParts(candidateParts)) {
      return candidate;
    }

    const candidateUtcTime = Date.UTC(
      candidateParts.year,
      candidateParts.month - 1,
      candidateParts.day,
      candidateParts.hours,
      candidateParts.minutes,
    );
    const delta = candidateUtcTime - desiredUtcTime;

    if (delta === 0) {
      return candidate;
    }

    candidate = new Date(candidate.getTime() - delta);
  }

  return matchesRequestedParts(getTimeZoneDateTimeParts(candidate, timeZone)) ? candidate : null;
};
