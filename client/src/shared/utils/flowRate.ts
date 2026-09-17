import type { FlowUnit } from "@/shared/features/preferences/types";

const LITERS_PER_US_GALLON = 3.785411784;

export const formatFlowRate = (litersPerMinute: number | null | undefined, unit: FlowUnit): string => {
  if (litersPerMinute == null || !Number.isFinite(litersPerMinute)) return "—";
  const value = unit === "GPM" ? litersPerMinute / LITERS_PER_US_GALLON : litersPerMinute;
  return `${value.toFixed(1)} ${unit}`;
};
