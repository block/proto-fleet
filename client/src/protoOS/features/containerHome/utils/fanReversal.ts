import type { FanReversalOutcome, FanReversalReadback } from "../hooks/useFanReversalOutcome";
import { fanReversalAwaitsReading, isFanReversalActive } from "../hooks/useFanReversalOutcome";
import {
  FAN_REVERSAL_MAX_MINUTES,
  FAN_REVERSAL_MIN_MINUTES,
  FAN_REVERSAL_OUTCOME_UNKNOWN,
} from "@/protoOS/api/adapters/fanReversalMessages";
import type { DryCoolerFanReversalStatus, MinerAdapter } from "@/protoOS/api/adapters/types";

export const FAN_REVERSAL_DURATIONS = Array.from(
  { length: FAN_REVERSAL_MAX_MINUTES - FAN_REVERSAL_MIN_MINUTES + 1 },
  (_, index) => FAN_REVERSAL_MIN_MINUTES + index,
);
export const DEFAULT_FAN_REVERSAL_MINUTES = 5;

// Frigate waits up to 15 seconds for the PLC to start the procedure, then up to the duration plus
// two minutes (at most 12) for it to end. ProtoOS waits a little longer so the controller, which
// reads the PLC, decides the outcome; only a silent connection reaches this deadline.
export const fanReversalDeadlineMs = (durationMinutes: number) =>
  15_000 + Math.min(durationMinutes + 2, 12) * 60_000 + 30_000;

export const HOSTED_FAN_REVERSAL_REASON = "Running a fan reversal needs a direct connection to the controller.";

const minutes = (count: number) => `${count} minute${count === 1 ? "" : "s"}`;

export const FAN_REVERSAL_STATE_LABELS: Record<DryCoolerFanReversalStatus["state"], string> = {
  stopped: "Stopped",
  starting: "Starting",
  inProgress: "Running",
  finished: "Finished, on standby",
  unknown: "Unknown",
};

export type FanReversalAvailability = { available: true } | { available: false; reason?: string };

export const fanReversalAvailability = ({
  adapter,
  isFleetHosted,
  outcome,
  readback,
  settingsPending,
}: {
  adapter?: Pick<MinerAdapter, "runDryCoolerFanReversal"> | null;
  isFleetHosted: boolean;
  /** True while a System controls change is still being saved. */
  settingsPending?: boolean;
  outcome?: FanReversalOutcome;
  readback?: FanReversalReadback;
}): FanReversalAvailability => {
  if (isFleetHosted) return { available: false, reason: HOSTED_FAN_REVERSAL_REASON };
  if (!adapter?.runDryCoolerFanReversal)
    return { available: false, reason: "This connection cannot run a fan reversal." };
  // The live state is shown beside the button while this run is open.
  if (outcome?.phase === "running") return { available: false };
  // A change still being saved could reach the PLC during the procedure.
  if (settingsPending) return { available: false, reason: "Wait for the current setting to save." };
  const status = readback?.status;
  if (!status) return { available: false, reason: "Waiting for the controller to report the fan reversal state." };
  if (isFanReversalActive(status))
    return { available: false, reason: "The controller reports a fan reversal running." };
  if (status.state === "unknown")
    return { available: false, reason: "Unavailable until the controller reports the fan reversal on standby." };
  // An uncertain run is never repeated on stale evidence: wait for a reading requested after it.
  if (fanReversalAwaitsReading(outcome, readback))
    return { available: false, reason: "Waiting for a new controller reading before another run." };
  return { available: true };
};

type FailedWrite =
  { message?: string; writeOutcome?: "refused" | "unknown"; status?: number; error?: { code?: number } } | undefined;

const modeLabel = (auto: boolean) => (auto ? "Auto" : "Manual");

/**
 * Decides the outcome from the controller's answer alone; the request is never repeated. The
 * dry cooler's mode is compared with a reading taken after the run, since ProtoOS never writes it.
 */
export const fanReversalOutcome = ({
  durationMinutes,
  result,
  error,
  modeBefore,
  modeAfter,
  nowMs,
}: {
  durationMinutes: number;
  result?: DryCoolerFanReversalStatus;
  error?: unknown;
  /** `dry_cooler_auto` before the run, unset when unknown. */
  modeBefore?: boolean;
  /** `dry_cooler_auto` read after the run; `null` when that read failed. */
  modeAfter?: boolean | null;
  nowMs: number;
}): FanReversalOutcome => {
  if (error !== undefined || !result) {
    const failed = error as FailedWrite;
    if (failed?.writeOutcome === "refused" && failed.message)
      return { phase: "failed", kind: "refused", message: failed.message };
    // Authentication failed before the controller processed anything.
    if (failed?.status === 401 || failed?.error?.code === 16)
      return {
        phase: "failed",
        kind: "refused",
        message: "Sign in again before trying again. The fan reversal did not start.",
      };
    return { phase: "unknown", message: FAN_REVERSAL_OUTCOME_UNKNOWN, settledAtMs: nowMs };
  }
  if (result.state === "stopped") {
    return {
      phase: "failed",
      kind: "stopped",
      message:
        "The fan reversal stopped before the controller reported it finished. Check the dry cooler and its alarms before running it again.",
    };
  }
  if (result.state !== "finished") {
    return { phase: "unknown", message: FAN_REVERSAL_OUTCOME_UNKNOWN, settledAtMs: nowMs };
  }
  const ran = result.durationMinutes ?? durationMinutes;
  const finished = `The controller reports the fan reversal finished after ${minutes(ran)}.`;
  if (modeBefore === undefined) return { phase: "completed", message: finished, tone: "neutral" };
  if (modeAfter === null)
    return {
      phase: "completed",
      message: `${finished} The dry cooler mode could not be re-read; check it is still ${modeLabel(modeBefore)}.`,
      tone: "warning",
    };
  if (modeAfter !== undefined && modeAfter !== modeBefore)
    return {
      phase: "completed",
      message: `${finished} The dry cooler mode now reads ${modeLabel(modeAfter)}; it was ${modeLabel(modeBefore)} before the run.`,
      tone: "warning",
    };
  return { phase: "completed", message: finished, tone: "neutral" };
};
