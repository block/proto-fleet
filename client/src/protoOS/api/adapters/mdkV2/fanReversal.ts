import type { DryCoolerFanReversalState, DryCoolerFanReversalStatus } from "../types";
import { V2DryCoolerFanReverseState, type V2DryCoolerFanReverseStatus } from "./generated/generatedApi";

const STATES: Partial<Record<string, DryCoolerFanReversalState>> = {
  [V2DryCoolerFanReverseState.DRY_COOLER_FAN_REVERSE_STATE_STOPPED]: "stopped",
  [V2DryCoolerFanReverseState.DRY_COOLER_FAN_REVERSE_STATE_STARTING]: "starting",
  [V2DryCoolerFanReverseState.DRY_COOLER_FAN_REVERSE_STATE_IN_PROGRESS]: "inProgress",
  [V2DryCoolerFanReverseState.DRY_COOLER_FAN_REVERSE_STATE_FINISHED]: "finished",
};

/**
 * PLC readback for the fan reversal. Unspecified, unknown and unrecognised states are `unknown`, and a
 * zero duration (a JSON gateway's omitted default) is treated as unreported.
 */
export const toFanReversalStatus = (wire?: V2DryCoolerFanReverseStatus): DryCoolerFanReversalStatus | undefined => {
  if (!wire) return undefined;
  const durationMinutes =
    typeof wire.durationMinutes === "number" && wire.durationMinutes > 0 ? wire.durationMinutes : undefined;
  return { state: (wire.state && STATES[wire.state]) ?? "unknown", ...(durationMinutes ? { durationMinutes } : {}) };
};
