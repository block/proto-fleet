import { create } from "zustand";
import { OUTCOME_TONES } from "./usePlantAlarmOutcome";
import type { ContainerSettings, DryCoolerFanReversalStatus } from "@/protoOS/api/adapters/types";
import useMinerStore from "@/protoOS/store/useMinerStore";

export type FanReversalOutcome =
  /** The request is open; the controller answers only when the PLC procedure ends. */
  | { phase: "running"; durationMinutes: number }
  | { phase: "completed"; message: string; tone: "neutral" | "warning" }
  /** `refused`: nothing started. `stopped`: the procedure started but ended without finishing. */
  | { phase: "failed"; kind: "refused" | "stopped"; message: string }
  /**
   * The procedure may or may not have run; `settledAtMs` marks when the request ended. Dismissing
   * hides the message only: the outcome stays so another run still waits for a newer reading.
   */
  | { phase: "unknown"; message: string; settledAtMs: number; dismissed?: boolean };

/** The latest controller readback of the procedure, and when ProtoOS asked for it. */
export interface FanReversalReadback {
  status?: DryCoolerFanReversalStatus;
  readAtMs: number;
}

// The outcome outlives the System controls modal: a run lasts minutes, and closing the modal must
// not hide it or abort the request. Both are scoped to the controller connection they came from.
const useFanReversalStore = create<{
  generation?: number;
  outcome?: FanReversalOutcome;
  readback?: FanReversalReadback;
  /** The controller read taken after the last run settled; `settings` is unset when it failed. */
  postRun?: { settings?: ContainerSettings };
}>(() => ({}));

export const setFanReversalOutcome = (generation: number, outcome: FanReversalOutcome | undefined) =>
  useFanReversalStore.setState((state) => ({
    generation,
    outcome,
    readback: state.generation === generation ? state.readback : undefined,
    postRun: state.generation === generation ? state.postRun : undefined,
  }));

/** Records a run's outcome with the controller read taken after it. */
export const settleFanReversal = (generation: number, outcome: FanReversalOutcome, settings?: ContainerSettings) => {
  setFanReversalOutcome(generation, outcome);
  useFanReversalStore.setState({ postRun: { settings } });
};

/** Hides the outcome's message. An uncertain run keeps holding another run until a newer reading. */
export const dismissFanReversalOutcome = (generation: number) =>
  useFanReversalStore.setState((state) => {
    if (state.generation !== generation) return state;
    return { outcome: state.outcome?.phase === "unknown" ? { ...state.outcome, dismissed: true } : undefined };
  });

/** Keeps the readback requested last; an older read finishing late never replaces a newer one. */
export const recordFanReversalReadback = (generation: number, readback: FanReversalReadback) =>
  useFanReversalStore.setState((state) => {
    if (state.generation !== generation) return { generation, outcome: undefined, readback, postRun: undefined };
    if (state.readback && state.readback.readAtMs > readback.readAtMs) return state;
    return { readback };
  });

export const resetFanReversalOutcome = () =>
  useFanReversalStore.setState({ generation: undefined, outcome: undefined, readback: undefined, postRun: undefined });

export const isFanReversalActive = (status?: DryCoolerFanReversalStatus) =>
  status?.state === "starting" || status?.state === "inProgress";

export function useFanReversalOutcome() {
  const generation = useMinerStore((state) => state.connectionGeneration);
  const outcome = useFanReversalStore((state) => (state.generation === generation ? state.outcome : undefined));
  const readback = useFanReversalStore((state) => (state.generation === generation ? state.readback : undefined));
  const postRun = useFanReversalStore((state) => (state.generation === generation ? state.postRun : undefined));
  return { generation, outcome, readback, postRun };
}

/** True after an uncertain run until a reading requested after it settled arrives. */
export const fanReversalAwaitsReading = (outcome?: FanReversalOutcome, readback?: FanReversalReadback) =>
  outcome?.phase === "unknown" && (readback?.readAtMs ?? 0) <= outcome.settledAtMs;

/**
 * Why the dry cooler's settings must not be written now, or undefined when they can be. The PLC
 * owns the cooler while the procedure runs, and may still own it after an uncertain run until a
 * newer reading says otherwise: the same evidence that holds another run.
 */
export const dryCoolerHoldReason = (outcome?: FanReversalOutcome, readback?: FanReversalReadback) => {
  if (outcome?.phase === "running" || isFanReversalActive(readback?.status))
    return "Dry cooler settings are locked while the fan reversal runs.";
  if (fanReversalAwaitsReading(outcome, readback))
    return "Dry cooler settings are locked until the controller reports whether the fan reversal ended.";
  return undefined;
};

/** The hold reason for the connected controller, read at the moment a write is about to be sent. */
export const dryCoolerWriteHold = (name: string) => {
  if (!name.startsWith("dry_cooler")) return undefined;
  const state = useFanReversalStore.getState();
  if (state.generation !== useMinerStore.getState().connectionGeneration) return undefined;
  return dryCoolerHoldReason(state.outcome, state.readback);
};

/** Shown wherever a run this session sent is still open. */
export const fanReversalWaitingMessage = (durationMinutes: number) =>
  `Fan reversal sent for ${durationMinutes} min. Waiting for the controller to report that it ended…`;

/** The message for a settled outcome, or undefined when none is shown or the run is still going. */
export const fanReversalMessage = (
  outcome: FanReversalOutcome | undefined,
): { message: string; tone: keyof typeof OUTCOME_TONES } | undefined => {
  if (!outcome || outcome.phase === "running") return undefined;
  if (outcome.phase === "completed") return { message: outcome.message, tone: outcome.tone };
  if (outcome.phase === "failed") return { message: outcome.message, tone: "critical" };
  return outcome.dismissed ? undefined : { message: outcome.message, tone: "warning" };
};
