import type { ModuleRunInRequest } from "@/protoOS/api/adapters/types";

export interface RunInDefaults {
  durationMinutes: number;
  sampleIntervalSeconds: number;
  cooldownMinutes: number;
  boardPowerTargetWatts: number;
  stopMiningOnFinish: boolean;
}

export type RunInDraft = Record<Exclude<keyof RunInDefaults, "stopMiningOnFinish">, string> & {
  stopMiningOnFinish: boolean;
};

export const toDraft = (defaults: RunInDefaults | null): RunInDraft => ({
  durationMinutes: defaults ? String(defaults.durationMinutes) : "",
  sampleIntervalSeconds: String(defaults?.sampleIntervalSeconds ?? 10),
  cooldownMinutes: String(defaults?.cooldownMinutes ?? 0),
  boardPowerTargetWatts: defaults ? String(defaults.boardPowerTargetWatts) : "",
  stopMiningOnFinish: defaults?.stopMiningOnFinish ?? true,
});

const MAX_DURATION_SECONDS = Math.floor(2 ** 63 / 1e9);
const MAX_POWER_WATTS = Math.floor((2 ** 32 - 1) / 1000);

export function validateDefaults(value: unknown): value is RunInDefaults {
  if (!value || typeof value !== "object") return false;
  const draft = value as Partial<RunInDefaults>;
  const integer = (value: unknown, min: number, max: number): value is number =>
    typeof value === "number" && Number.isSafeInteger(value) && value >= min && value <= max;
  return (
    integer(draft.durationMinutes, 1, Math.floor(MAX_DURATION_SECONDS / 60)) &&
    integer(draft.sampleIntervalSeconds, 1, MAX_DURATION_SECONDS) &&
    draft.sampleIntervalSeconds <= draft.durationMinutes * 60 &&
    integer(draft.cooldownMinutes, 0, Math.floor(MAX_DURATION_SECONDS / 60)) &&
    integer(draft.boardPowerTargetWatts, 1, MAX_POWER_WATTS) &&
    typeof draft.stopMiningOnFinish === "boolean"
  );
}

export function parseDraft(draft: RunInDraft): RunInDefaults {
  const value = {
    durationMinutes: Number(draft.durationMinutes),
    sampleIntervalSeconds: Number(draft.sampleIntervalSeconds),
    cooldownMinutes: Number(draft.cooldownMinutes),
    boardPowerTargetWatts: Number(draft.boardPowerTargetWatts),
    stopMiningOnFinish: draft.stopMiningOnFinish,
  };
  if (!draft.cooldownMinutes.trim() || !validateDefaults(value)) {
    throw new Error(
      "Enter whole numbers: duration, sample interval and power must be positive; cooldown can be zero. The sample interval must not exceed the run duration.",
    );
  }
  return value;
}

export const toRunInRequest = (defaults: RunInDefaults): ModuleRunInRequest => ({
  duration: `${defaults.durationMinutes * 60}s`,
  sampleInterval: `${defaults.sampleIntervalSeconds}s`,
  cooldownDuration: `${defaults.cooldownMinutes * 60}s`,
  boardPowerTargetWatts: defaults.boardPowerTargetWatts,
  stopMiningOnFinish: defaults.stopMiningOnFinish,
});
