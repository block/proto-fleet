/**
 * Durable module voltage setting as the controller stores it. Fixed and automatic modes apply to every
 * hashboard the controller currently places in the module.
 */
export type ModuleVoltageReadiness =
  "ready" | "limitsUnavailable" | "noMembers" | "targetOutsideLimits" | "noCommonRange" | "unknown";

export interface ModuleVoltageSetting {
  /** The controller's module identifier, as it reported it. */
  moduleId: string;
  /** Absent means automatic voltage tuning. */
  fixedMillivolts?: number;
  /**
   * Range every member board's firmware accepts. Absent when the controller could not establish one, so a
   * missing range is never read as zero or as a default.
   */
  supportedRange?: { minimumMillivolts: number; maximumMillivolts: number };
  boardCount: number;
  readiness: ModuleVoltageReadiness;
  readinessMessage?: string;
  updatedBy?: string;
  updatedAt?: Date;
}

export interface ModuleVoltageBoardResult {
  macAddress: string;
  /** False when the command did not reach the board; the controller keeps retrying the saved setting. */
  applied: boolean;
  message?: string;
}

export interface ModuleVoltageWriteResult {
  /** Saved setting returned with the write. Callers still confirm with a separate read. */
  setting?: ModuleVoltageSetting;
  boardsUpdated: number;
  boards: ModuleVoltageBoardResult[];
}

/** Largest value the uint32 millivolts field can carry. */
const MAX_WIRE_MILLIVOLTS = 4_294_967_295;

export const MODULE_VOLTAGE_OUTCOME_UNKNOWN =
  "The controller did not confirm whether the voltage setting changed. Check the current setting before trying again.";

export const isWireMillivolts = (value: number) => Number.isInteger(value) && value > 0 && value <= MAX_WIRE_MILLIVOLTS;

/** Module IDs are compared the way the controller normalizes them. */
export const sameModuleId = (a: string, b: string) => a.trim().toLowerCase() === b.trim().toLowerCase();

export const findModuleVoltageSetting = (settings: readonly ModuleVoltageSetting[], moduleId: string) =>
  settings.find((setting) => sameModuleId(setting.moduleId, moduleId));
