import { Timeframe } from "./types";

export const MONTH_NAMES = [
  "January",
  "February",
  "March",
  "April",
  "May",
  "June",
  "July",
  "August",
  "September",
  "October",
  "November",
  "December",
] as const;

export const MONTH_NAMES_SHORT = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec",
] as const;

export const DAY_NAMES_SHORT = ["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"] as const;

export const CALENDAR_ROWS = 6;
export const DAYS_IN_WEEK = 7;

export type PresetId = "today" | "yesterday" | "this-week" | "last-week" | "this-month" | "last-month" | "custom";

export interface BuiltInPreset {
  id: PresetId;
  label: string;
}

export const DEFAULT_PRESETS: BuiltInPreset[] = [
  { id: "today", label: "Today" },
  { id: "yesterday", label: "Yesterday" },
  { id: "this-week", label: "This Week" },
  { id: "last-week", label: "Last Week" },
  { id: "this-month", label: "This Month" },
  { id: "last-month", label: "Last Month" },
  { id: "custom", label: "Custom" },
];

export function isPresetVisibleForTimeframe(presetId: PresetId, timeframe?: Timeframe): boolean {
  if (!timeframe) return true;
  if (presetId === "today" || presetId === "custom") return true;
  if (timeframe === "present") return false;
  if (timeframe === "past" && (presetId === "this-week" || presetId === "this-month")) return false;
  if (timeframe === "future") return false;
  return true;
}
