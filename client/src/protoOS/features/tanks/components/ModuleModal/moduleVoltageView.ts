import {
  MODULE_VOLTAGE_OUTCOME_UNKNOWN,
  type ModuleVoltageSetting,
  type ModuleVoltageWriteResult,
} from "@/protoOS/api/adapters/moduleVoltage";

/** Requested mode: a fixed millivolt target, or undefined for automatic tuning. */
export type VoltageRequest = number | undefined;

export interface VoltageOutcome {
  tone: "neutral" | "warning" | "critical";
  message: string;
  /** Boards that did not apply the setting, or applied it with a note. */
  boards?: Array<{ label: string; message: string }>;
}

export const formatMillivolts = (value: number) => `${value.toLocaleString("en-US")} mV`;

export const formatRange = (range: NonNullable<ModuleVoltageSetting["supportedRange"]>) =>
  `${range.minimumMillivolts.toLocaleString("en-US")}–${formatMillivolts(range.maximumMillivolts)}`;

export const modeLabel = (setting: Pick<ModuleVoltageSetting, "fixedMillivolts">) =>
  setting.fixedMillivolts === undefined ? "Automatic" : `Fixed at ${formatMillivolts(setting.fixedMillivolts)}`;

const READINESS_NOTES: Partial<Record<ModuleVoltageSetting["readiness"], string>> = {
  limitsUnavailable: "Firmware has not reported voltage limits for every hashboard in this module.",
  noMembers: "The controller does not currently place any hashboards in this module.",
  targetOutsideLimits: "The saved voltage is outside the range these hashboards now support, so they are not using it.",
  noCommonRange: "The hashboards in this module have no supported voltage range in common.",
};

/** Explains why the setting cannot be applied as saved, or undefined when it can. */
export const readinessNote = (setting: ModuleVoltageSetting) =>
  setting.readiness === "ready" ? undefined : (READINESS_NOTES[setting.readiness] ?? setting.readinessMessage);

/** A fixed target can be chosen only inside a range the controller reported for every member board. */
export const canSetFixed = (setting: ModuleVoltageSetting | undefined) =>
  setting?.supportedRange !== undefined && setting.boardCount > 0;

/** Returns an error message, or the parsed millivolts. */
export const parseMillivolts = (
  input: string,
  range: NonNullable<ModuleVoltageSetting["supportedRange"]>,
): { error: string } | { millivolts: number } => {
  const text = input.trim().replace(/,/g, "");
  if (!/^\d+$/.test(text)) return { error: "Enter a whole number of millivolts." };
  const millivolts = Number(text);
  if (
    !Number.isSafeInteger(millivolts) ||
    millivolts < range.minimumMillivolts ||
    millivolts > range.maximumMillivolts
  ) {
    return { error: `Enter a value from ${formatRange(range)}.` };
  }
  return { millivolts };
};

const matches = (setting: ModuleVoltageSetting, request: VoltageRequest) => setting.fixedMillivolts === request;

const requestLabel = (request: VoltageRequest) =>
  request === undefined ? "automatic voltage" : `a fixed voltage of ${formatMillivolts(request)}`;

const boardNotes = (result: ModuleVoltageWriteResult, boardLabel: (mac: string) => string) =>
  result.boards
    .filter((board) => !board.applied || board.message)
    .map((board) => ({
      label: boardLabel(board.macAddress),
      message: board.applied ? (board.message ?? "") : `Not applied yet${board.message ? `: ${board.message}` : ""}`,
    }));

/**
 * Combines the write result with a separate re-read. The write is never repeated: a missing or failed answer
 * is resolved only by what the controller reports afterwards.
 */
export function voltageOutcome({
  request,
  write,
  previous,
  reread,
  boardLabel,
}: {
  request: VoltageRequest;
  write: { result: ModuleVoltageWriteResult } | { error: { message: string; writeOutcome?: "refused" | "unknown" } };
  /** The setting on screen when the write was sent, when one was loaded. */
  previous?: ModuleVoltageSetting | null;
  /** undefined when the re-read failed; null when the controller no longer lists the module. */
  reread: ModuleVoltageSetting | null | undefined;
  boardLabel: (mac: string) => string;
}): VoltageOutcome {
  const missing = "The controller no longer lists this module. It may have been replaced.";
  if ("error" in write) {
    if (write.error.writeOutcome === "refused") {
      // A refusal means nothing was saved. If the re-read disagrees, report what it shows instead.
      if (previous && reread && reread.fixedMillivolts !== previous.fixedMillivolts) {
        return {
          tone: "critical",
          message: `The controller refused the request, but it now reports ${requestLabel(reread.fixedMillivolts)}. Check the setting before trying again.`,
        };
      }
      return { tone: "critical", message: write.error.message };
    }
    if (reread === undefined) {
      return {
        tone: "critical",
        message: `${MODULE_VOLTAGE_OUTCOME_UNKNOWN} The current setting could not be re-read.`,
      };
    }
    if (reread === null) return { tone: "critical", message: `${MODULE_VOLTAGE_OUTCOME_UNKNOWN} ${missing}` };
    if (matches(reread, request)) {
      return {
        tone: "warning",
        message: `The controller did not answer, but it now reports ${requestLabel(request)}. Per-hashboard results are unavailable.`,
      };
    }
    return { tone: "critical", message: `${MODULE_VOLTAGE_OUTCOME_UNKNOWN} The current setting is shown above.` };
  }

  if (reread === undefined) {
    return {
      tone: "warning",
      message:
        "The controller saved the change, but the current setting could not be re-read. Reload before changing it again.",
    };
  }
  if (reread === null) return { tone: "critical", message: missing };
  if (!matches(reread, request)) {
    return {
      tone: "critical",
      message: `The controller now reports ${requestLabel(reread.fixedMillivolts)}, not ${requestLabel(request)}. Check the setting before trying again.`,
    };
  }
  const notes = boardNotes(write.result, boardLabel);
  const pending = write.result.boards.filter((board) => !board.applied).length;
  const total = write.result.boards.length;
  const saved =
    request === undefined ? "Automatic voltage restored" : `Fixed voltage of ${formatMillivolts(request)} saved`;
  if (pending > 0) {
    return {
      tone: "warning",
      message: `${saved}, but ${pending} of ${total} hashboards ${pending === 1 ? "has" : "have"} not applied it yet. The controller keeps retrying.`,
      boards: notes,
    };
  }
  return {
    tone: "neutral",
    message:
      total > 0 ? `${saved} and applied to ${total === 1 ? "1 hashboard" : `all ${total} hashboards`}.` : `${saved}.`,
    ...(notes.length ? { boards: notes } : {}),
  };
}
