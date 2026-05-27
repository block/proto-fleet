export const unavailableCurtailmentTimeLabel = "Time unavailable";

const millisecondsPerSecond = 1000;
const dateTimeFormatter = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  hour: "numeric",
  minute: "2-digit",
});

export function getCurtailmentDateTime(value?: string): Date | undefined {
  if (!value) {
    return undefined;
  }

  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date;
}

export function formatCurtailmentDateTimeValue(date: Date): string {
  return dateTimeFormatter.format(date);
}

export function formatCurtailmentDateTime(value?: string): string | undefined {
  const date = getCurtailmentDateTime(value);
  return date ? formatCurtailmentDateTimeValue(date) : undefined;
}

export function formatCurtailmentDateTimeOrFallback(
  value?: string,
  fallback = unavailableCurtailmentTimeLabel,
): string {
  return formatCurtailmentDateTime(value) ?? fallback;
}

export function formatCurtailmentEstimatedCompletion(remainingSeconds: number, currentTime = new Date()): string {
  if (!Number.isFinite(remainingSeconds)) {
    return unavailableCurtailmentTimeLabel;
  }

  const currentTimeMs = currentTime.getTime();
  const estimatedCompletionMs = currentTimeMs + Math.max(remainingSeconds, 0) * millisecondsPerSecond;

  if (!Number.isFinite(currentTimeMs) || !Number.isFinite(estimatedCompletionMs)) {
    return unavailableCurtailmentTimeLabel;
  }

  const estimatedCompletionDate = new Date(estimatedCompletionMs);
  return Number.isNaN(estimatedCompletionDate.getTime())
    ? unavailableCurtailmentTimeLabel
    : formatCurtailmentDateTimeValue(estimatedCompletionDate);
}
