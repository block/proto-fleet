/**
 * Types for status summary computation
 *
 * These types define a normalized error format that both ProtoOS and ProtoFleet
 * transform their data into before calling the shared status hooks.
 */

/**
 * Component types supported for status computation
 */
export type StatusComponentType = "hashboard" | "psu" | "fan" | "controlBoard" | "other";

/**
 * Normalized error for status computation
 * Both ProtoOS and ProtoFleet transform their errors to this format
 */
export interface StatusError {
  componentType: StatusComponentType;
  slot?: number; // 1-based slot number
}

/**
 * Grouped errors by component type - input to status computation hooks
 */
export interface GroupedStatusErrors {
  hashboard: StatusError[];
  psu: StatusError[];
  fan: StatusError[];
  controlBoard: StatusError[];
  other: StatusError[];
}

/**
 * Unified miner status summary - output from useMinerStatusSummary hook
 *
 * @property condensed - Short status for chips/columns (e.g., "Hashing", "Hashboard 1 issue")
 * @property title - Modal header title (e.g., "All systems are operational", "Hashboard 1 issue")
 * @property subtitle - Optional subtitle for additional context (currently unused)
 */
export interface MinerStatusSummary {
  condensed: string;
  title: string;
  subtitle?: string;
}

/**
 * Component status summary - output from useComponentStatusSummary hook
 *
 * @property title - Component modal header title, or null if single error (show error message instead)
 * @property subtitle - Optional subtitle for additional context (currently unused)
 */
export interface ComponentStatusSummary {
  title: string | null;
  subtitle?: string;
}
