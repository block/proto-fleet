import { useEffect, useState } from "react";
import { create } from "zustand";
import { type AlarmAction, type AlarmReadback, alarmReadback } from "../utils/plantAlarms";
import type { PlantTelemetry } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

export type PlantAlarmOutcome =
  | { phase: "sending"; action: AlarmAction }
  | {
      phase: "sent";
      action: AlarmAction;
      /** Controller time of the plant reading held at acknowledgement. */
      baselineObservedAtMs?: number;
      /** The store's request sequence at acknowledgement; only later requests can decide the outcome. */
      afterRequestId: number;
      acknowledgedAtMs: number;
    }
  /** The first decisive readback, kept so a later, unrelated reading cannot change it. */
  | { phase: "settled"; action: AlarmAction; confirmed: boolean; message: string }
  | { phase: "failed"; action: AlarmAction; kind: "refused" | "unknown"; message: string };

// The outcome outlives the alarms section: closing the modal or leaving the page must not hide an
// unconfirmed command. It is scoped to the controller connection it was sent on.
const useOutcomeStore = create<{ generation?: number; outcome?: PlantAlarmOutcome }>(() => ({}));

export const setPlantAlarmOutcome = (generation: number, outcome: PlantAlarmOutcome | undefined) =>
  useOutcomeStore.setState({ generation, outcome });

export function usePlantAlarmOutcome() {
  const generation = useMinerStore((state) => state.connectionGeneration);
  const outcome = useOutcomeStore((state) => (state.generation === generation ? state.outcome : undefined));
  return { generation, outcome };
}

const plantId = (state: ReturnType<typeof useMinerStore.getState>) =>
  [...state.hardware.plant.values()].find((component) => component.kind === "plant")?.ref.id;

export const usePlant = (): PlantTelemetry | undefined =>
  useMinerStore((state) => {
    const id = plantId(state);
    return id ? state.telemetry.plant.get(id) : undefined;
  });

/** The request that produced the current plant reading, from the store's per-entity write gate. */
const usePlantReadingRequestId = (): number | undefined =>
  useMinerStore((state) => {
    const id = plantId(state);
    return id ? state.telemetry.lastAppliedRequestByEntity.get(`plant:${id}`) : undefined;
  });

/** Records the acknowledgement point: the reading held now, and the requests already started. */
export const acknowledgedOutcome = (action: AlarmAction): Extract<PlantAlarmOutcome, { phase: "sent" }> => {
  const state = useMinerStore.getState();
  const id = plantId(state);
  return {
    phase: "sent",
    action,
    baselineObservedAtMs: id ? state.telemetry.plant.get(id)?.observedAtMs : undefined,
    afterRequestId: state.requestSequence,
    acknowledgedAtMs: Date.now(),
  };
};

// Keeps observation ages and the readback windows current between polls.
export const useNow = () => {
  const [nowMs, setNowMs] = useState(Date.now);
  useEffect(() => {
    const id = setInterval(() => setNowMs(Date.now()), 1_000);
    return () => clearInterval(id);
  }, []);
  return nowMs;
};

/**
 * Judges a sent command from later plant readings and latches the first decisive result, so every
 * reader shows the same outcome and an unrelated later alarm cannot flip it. Only "no newer
 * reading yet" stays open.
 */
export function usePlantAlarmReadback(nowMs: number) {
  const { generation, outcome } = usePlantAlarmOutcome();
  const plant = usePlant();
  const readingRequestId = usePlantReadingRequestId();
  const readback: AlarmReadback | undefined =
    outcome?.phase === "sent" ? alarmReadback({ ...outcome, readingRequestId, nowMs, plant }) : undefined;
  const decided = readback?.state === "confirmed" || readback?.state === "unconfirmed" ? readback : undefined;
  useEffect(() => {
    if (outcome?.phase !== "sent" || !decided) return;
    const current = useOutcomeStore.getState();
    // Another reader may have latched it already.
    if (current.generation !== generation || current.outcome !== outcome) return;
    setPlantAlarmOutcome(generation, {
      phase: "settled",
      action: outcome.action,
      confirmed: decided.state === "confirmed",
      message: decided.message,
    });
  }, [decided, generation, outcome]);
  return { generation, outcome, readback, plant };
}

export const OUTCOME_TONES = {
  neutral: "text-text-primary",
  warning: "text-intent-warning-text",
  critical: "text-text-critical",
} as const;

/** The message to show for an outcome, or undefined while it is still being sent or confirmed. */
export const outcomeMessage = (
  outcome: PlantAlarmOutcome | undefined,
  readback: AlarmReadback | undefined,
): { message: string; tone: "neutral" | "warning" | "critical" } | undefined => {
  if (outcome?.phase === "failed")
    return { message: outcome.message, tone: outcome.kind === "refused" ? "critical" : "warning" };
  if (outcome?.phase === "settled")
    return { message: outcome.message, tone: outcome.confirmed ? "neutral" : "warning" };
  if (readback && readback.state !== "waiting")
    return { message: readback.message, tone: readback.state === "confirmed" ? "neutral" : "warning" };
  return undefined;
};
