import type { BoardTemperatureSnapshot, PsuObservationTime } from "../contract";

// CT1 health observations expire after two minutes. Polling does not renew them.
export const HEALTH_MAX_AGE_MS = 120_000;
const MAX_FUTURE_SKEW_MS = 5_000;
const MAX_BROWSER_CLOCK_GAP_MS = 1_000;
export type ReadingStatus = "current" | "stale" | "unavailable";

export function readingStatus(at: number | undefined, now = Date.now()): ReadingStatus {
  if (!Number.isFinite(now) || at === undefined || !Number.isFinite(at) || at > now + MAX_FUTURE_SKEW_MS)
    return "unavailable";
  return now - at >= HEALTH_MAX_AGE_MS ? "stale" : "current";
}

/** Keep the enclosing frame and the adjusted sample time for later freshness checks. */
export function sourceObservationTime(timestamp: string | undefined, age = 0): PsuObservationTime | undefined {
  const frameAt = Date.parse(timestamp ?? "");
  if (!Number.isFinite(frameAt) || !Number.isFinite(age) || age < 0) return undefined;
  return { frameAt, observedAt: frameAt - age };
}

type BrowserClockReading = { localMs: number; wallMs: number };
type ClockObservation = { requestId: number; start: BrowserClockReading };

// On some systems only the wall clock advances during sleep.
const browserClockGap = (start: BrowserClockReading, end: BrowserClockReading) =>
  end.wallMs - start.wallMs - (end.localMs - start.localMs);

/** One clock per CT1 connection. The controller evaluates mining status during the request. */
export class ControllerClock {
  private requestId = 0;
  private latestRequestId = 0;
  private latestEvaluation?: number;
  private sample?: { controllerMs: number; localMs: number };
  private browserReading?: BrowserClockReading;
  private pendingGap?: "timer" | "resume";
  supportsEvaluationTime?: boolean;

  startObservation(localMs = performance.now(), wallMs = Date.now()): ClockObservation {
    const start = { localMs, wallMs };
    // A regular poll can start after wake, before the browser delivers its resume event.
    this.recordBrowserTime(start);
    return { requestId: ++this.requestId, start };
  }

  observe(
    timestamp: string | undefined,
    { requestId, start }: ClockObservation,
    receivedAt = performance.now(),
    wallMs = Date.now(),
  ) {
    if (requestId <= this.latestRequestId) return;
    this.latestRequestId = requestId;
    this.supportsEvaluationTime = timestamp != null;
    const controllerMs = Date.parse(timestamp ?? "");
    if (!Number.isFinite(controllerMs)) {
      this.sample = undefined;
      return;
    }
    // A repeated evaluation is the same sample, even if the client fetched it again.
    if (controllerMs === this.latestEvaluation) return;
    const halfTrip = (receivedAt - start.localMs) / 2;
    // The midpoint bounds the estimate's error to half the request duration.
    if (
      halfTrip > MAX_FUTURE_SKEW_MS ||
      Math.abs(browserClockGap(start, { localMs: receivedAt, wallMs })) > MAX_BROWSER_CLOCK_GAP_MS
    )
      return;
    this.latestEvaluation = controllerMs;
    this.sample = { controllerMs, localMs: start.localMs + halfTrip };
    this.pendingGap = undefined;
  }

  private recordBrowserTime(reading: BrowserClockReading) {
    const previous = this.browserReading;
    this.browserReading = reading;
    if (!previous) return;
    const gap = browserClockGap(previous, reading);
    if (Math.abs(gap) <= MAX_BROWSER_CLOCK_GAP_MS) return;
    this.latestRequestId = this.requestId;
    // A backward correction alone need not trigger a foreground request. Remember
    // it in case the browser delivers a resume event before the next good poll.
    if (gap > 0) this.pendingGap = "timer";
    else this.pendingGap ??= "resume";
  }

  checkForDiscontinuity(trigger: "timer" | "resume", localMs = performance.now(), wallMs = Date.now()): boolean {
    this.recordBrowserTime({ localMs, wallMs });
    if (!this.pendingGap || (trigger === "timer" && this.pendingGap === "resume")) return false;
    this.pendingGap = undefined;
    this.sample = undefined;
    return true;
  }

  now(localMs = performance.now()): number {
    return this.sample ? this.sample.controllerMs + localMs - this.sample.localMs : NaN;
  }
}

export function boardTemperatures(samples: BoardTemperatureSnapshot["samples"], now: number) {
  const values: number[] = [];
  let nextChangeAt = Infinity;
  if (Number.isFinite(now)) {
    for (const sample of samples) {
      const status = readingStatus(sample.at, now);
      if (status === "current") {
        values.push(...sample.values);
        nextChangeAt = Math.min(nextChangeAt, sample.at + HEALTH_MAX_AGE_MS);
      } else if (status === "unavailable") {
        nextChangeAt = Math.min(nextChangeAt, sample.at - MAX_FUTURE_SKEW_MS);
      }
    }
  }
  const metric = (value: number | null) => ({ latest: { value, units: "°C" as const } });
  return {
    boardTemperatureSnapshot: { samples, evaluatedAt: now, nextChangeAt },
    temperature: metric(values.length ? values.reduce((a, b) => a + b, 0) / values.length : null),
    minimumTemperature: metric(values.length ? Math.min(...values) : null),
    maximumTemperature: metric(values.length ? Math.max(...values) : null),
  };
}
