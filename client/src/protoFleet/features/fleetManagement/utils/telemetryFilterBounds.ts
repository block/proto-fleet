import { NumericField } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type { NumericRangeBounds } from "@/shared/utils/filterValidation";

/**
 * Per-field logical bounds + display labels for the numeric range filters.
 * Single source of truth: filter definitions, validators, and placeholder hints
 * all read from this. Units are display units that match what the telemetry
 * APIs already emit (TH/s, J/TH, kW, °C), so a filter input of "100" lines up
 * with the same number rendered in the miner-list cells.
 *
 * Bounds are generous (S21 XP tops out around 270 TH/s; we leave headroom for
 * next-gen hardware and anomalous readings).
 */
export const TELEMETRY_FILTER_BOUNDS = {
  hashrate: { min: 0, max: 1000, unit: "TH/s", label: "Hashrate" },
  efficiency: { min: 0, max: 100, unit: "J/TH", label: "Efficiency" },
  power: { min: 0, max: 50, unit: "kW", label: "Power" },
  temperature: { min: 0, max: 150, unit: "°C", label: "Temperature" },
} as const satisfies Record<string, NumericRangeBounds & { label: string }>;

export type TelemetryFilterKey = keyof typeof TELEMETRY_FILTER_BOUNDS;

/**
 * Maps a `NumericField` proto enum value to the matching bounds entry, or
 * undefined for fields the UI doesn't surface in v1 (UNSPECIFIED, voltage,
 * current — backend supports them but the dropdown excludes them per #139).
 */
export const telemetryFilterBoundsForProtoField = (
  field: NumericField,
): (typeof TELEMETRY_FILTER_BOUNDS)[TelemetryFilterKey] | undefined => {
  switch (field) {
    case NumericField.HASHRATE_THS:
      return TELEMETRY_FILTER_BOUNDS.hashrate;
    case NumericField.EFFICIENCY_JTH:
      return TELEMETRY_FILTER_BOUNDS.efficiency;
    case NumericField.POWER_KW:
      return TELEMETRY_FILTER_BOUNDS.power;
    case NumericField.TEMPERATURE_C:
      return TELEMETRY_FILTER_BOUNDS.temperature;
    case NumericField.UNSPECIFIED:
    case NumericField.VOLTAGE_V:
    case NumericField.CURRENT_A:
    default:
      return undefined;
  }
};

/**
 * Inverse map: telemetry filter key → proto NumericField. Used by the
 * MinerList serializer to turn ActiveFilters numericFilters into proto
 * NumericRangeFilter entries.
 */
export const protoFieldForTelemetryKey: Record<TelemetryFilterKey, NumericField> = {
  hashrate: NumericField.HASHRATE_THS,
  efficiency: NumericField.EFFICIENCY_JTH,
  power: NumericField.POWER_KW,
  temperature: NumericField.TEMPERATURE_C,
};
