import type { BoardEvent } from "./types";

/** "HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE" with its prefix becomes "Board over temperature". */
export const humanizeCode = (code: string, prefix: string) => {
  const words = code.replace(prefix, "").replaceAll("_", " ").toLowerCase().trim();
  return (words.charAt(0).toUpperCase() + words.slice(1)).replace(/\basic\b/gi, "ASIC");
};

const SOURCES: Record<string, string> = { asic_frames: "ASIC frames", can_status: "CAN status" };

/** Names an evidence document section, such as asic_frames. */
export const sourceLabel = (key: string) => SOURCES[key] ?? humanizeCode(key, "");

// The controller sends an unset enum as its _UNSPECIFIED value, which reads the same as a missing one.
const known = (code: string | undefined) => (code && !code.endsWith("_UNSPECIFIED") ? code : undefined);

export const causeLabel = (cause: string | undefined) =>
  humanizeCode(known(cause) ?? "HASHBOARD_ERROR_CAUSE_UNKNOWN_ERROR", "HASHBOARD_ERROR_CAUSE_");

export const miningStateLabel = (state: string | undefined) => {
  const code = known(state);
  return code ? humanizeCode(code, "MINING_STATE_") : "Unknown";
};

/** Stack numbers are the firmware's own zero-based index, so the label says so. */
export const stackLabel = (stack: number | undefined) =>
  stack === undefined ? "Stack" : `Stack ${stack} (firmware index)`;

const TRIGGERS: Record<string, string> = {
  power_recovery: "Power recovery",
  power_recovery_capture_only: "Captured without power recovery",
  ready_recovery: "Ready recovery",
};

export const triggerLabel = (trigger: string | undefined) =>
  trigger ? (TRIGGERS[trigger] ?? humanizeCode(trigger, "")) : "Unknown";

/** One line for an event row. Engineering detail stays out. */
export const eventLabel = (event: BoardEvent) => {
  switch (event.kind) {
    case "stateChange":
      return `Mining state ${miningStateLabel(event.fromState)} → ${miningStateLabel(event.toState)}`;
    case "error":
      return `Error: ${causeLabel(event.cause)}`;
    case "warning":
      return `Warning: ${causeLabel(event.cause)}`;
    case "lastShutdown":
      return `Last shutdown: ${causeLabel(event.cause)}`;
    case "stackDropout":
      return event.stack === undefined ? "Stack dropout" : `${stackLabel(event.stack)} dropped out`;
  }
};
