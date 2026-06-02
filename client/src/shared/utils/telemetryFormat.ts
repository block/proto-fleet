// Telemetry conversions + display formatters. Single home for the unit
// math (MH→TH, GH→TH, W→kW, °C↔°F) and the display strings built on
// top of them. Every surface that renders telemetry — /sites,
// /buildings/:id, BuildingCard footer, HashRateValue, rack cards,
// the protoOS popovers — should source these from here so the unit
// ladder, decimal precision, and em-dash fallbacks stay consistent.

import { type TemperatureUnit } from "@/shared/features/preferences";
import { getDisplayValue, separateByCommas } from "@/shared/utils/stringUtils";

export const KW_PER_MW = 1_000;

// ---------------------------------------------------------------------------
// Unit conversions
// ---------------------------------------------------------------------------

export const convertMegahashSecToTerahashSec = (value?: number | null) => (value ?? 0) / 1_000_000;
export const convertGigahashSecToTerahashSec = (value?: number | null) => (value ?? 0) / 1_000;
export const convertWtoKW = (value?: number | null) => (value ?? 0) / 1_000;

export const convertCtoF = (value: number = 0) => (value * 9) / 5 + 32;
export const convertFtoC = (value: number = 0) => ((value - 32) * 5) / 9;

// ---------------------------------------------------------------------------
// Hashrate display
// ---------------------------------------------------------------------------

// Hashrate unit ladder boundary constants. The simpler "if > 1000, scale
// everything by 1000" pattern in the dashboard chart code uses these so
// the per-series scaling matches what `formatHashrateWithUnit` would
// pick for the max. Kept exported for that callsite.
export const TH_TO_PH_THRESHOLD = 1_000;
export const TH_TO_PH_DIVISOR = 1_000;

// Internal ladder constants for formatHashrateWithUnit. Identical to the
// exported pair above for the TH→PH step; named differently here to make
// the auto-scaling staircase (GH/TH/PH/EH) explicit.
const TH_PER_PH = 1_000;
const TH_PER_EH = 1_000_000;
const GH_PER_TH = 1_000;

// Picks the smallest unit (GH/TH/PH/EH) that keeps the displayed value in
// [1, 1000) for non-zero inputs. Zero stays in TH/s as the conventional
// "no signal" default. Used directly by surfaces that need both the value
// and unit (HashRateValue, AsicPopover); higher-level helpers like
// `formatHashrate` compose this with separator + decimal rules.
export const formatHashrateWithUnit = (value: number = 0) => {
  // NaN/Infinity guard. Bad upstream data (corrupted telemetry, division
  // by zero in an aggregation) shouldn't propagate `NaN TH/s` to the UI;
  // fall through to the zero rendering instead.
  if (!Number.isFinite(value)) {
    return { value: 0, unit: "TH/S" };
  }
  if (value <= 0) {
    return { value: 0, unit: "TH/S" };
  }
  // Strict `>` boundaries keep prior callers (AsicPopover, HashRateValue)
  // rendering 1000 TH/s as "1000 TH/S" instead of "1 PH/S"; same rule
  // applies one step up at the EH boundary.
  if (value > TH_PER_EH) {
    return { value: value / TH_PER_EH, unit: "EH/S" };
  }
  if (value > TH_PER_PH) {
    return { value: value / TH_PER_PH, unit: "PH/S" };
  }
  if (value < 1) {
    return { value: value * GH_PER_TH, unit: "GH/S" };
  }
  return { value, unit: "TH/S" };
};

// Auto-scaled hashrate string with comma-separated thousands and
// magnitude-aware decimal precision. Returns `null` for `null` input so
// callers that render skeletons vs em-dashes can disambiguate; use the
// `OrDash` variant below for compact tiles that always need a string.
export const formatHashrate = (hashrateTh: number | null): string | null => {
  if (hashrateTh === null) return null;
  if (hashrateTh === 0) return "0 TH/s";
  const { value, unit } = formatHashrateWithUnit(hashrateTh);
  // Two decimals when scaled value is < 10 so small magnitudes keep signal
  // (e.g. 2.50 EH/s); one decimal above that to match the dashboard bar.
  const decimals = value < 10 ? 2 : 1;
  // formatHashrateWithUnit returns uppercase units (PH/S); the metric row
  // uses lowercase /s throughout.
  return `${separateByCommas(value.toFixed(decimals))} ${unit.replace("/S", "/s")}`;
};

export const formatHashrateOrDash = (hashrateTh: number | null): string => formatHashrate(hashrateTh) ?? "—";

// ---------------------------------------------------------------------------
// Power display
// ---------------------------------------------------------------------------

// "12.3 / 20.0 MW" — used/capacity pair with em-dash fallback per side.
// `usedKw` is in kilowatts; `capacityMw` in megawatts (matches the proto
// shape on Site / Building).
export const formatPowerUsedCapacity = (usedKw: number | null, capacityMw: number): string | null => {
  const hasCapacity = capacityMw > 0;
  if (usedKw === null && !hasCapacity) return null;
  const usedMw = usedKw !== null ? usedKw / KW_PER_MW : null;
  const usedText = usedMw !== null ? usedMw.toFixed(1) : "—";
  const capacityText = hasCapacity ? capacityMw.toFixed(1) : "—";
  return `${usedText} / ${capacityText} MW`;
};

// Total power as MW with em-dash fallback. Site/building rows use
// `formatPowerUsedCapacity` for used/capacity strings; this single-value
// variant is for footers that don't carry a capacity.
export const formatPowerMwOrDash = (powerKw: number | null): string => {
  if (powerKw === null) return "—";
  const mw = powerKw / KW_PER_MW;
  return `${separateByCommas(mw.toFixed(1))} MW`;
};

// Total power as kW with em-dash fallback. Used by smaller-scope
// surfaces (rack cards, single-device popovers) where MW would round to
// "0.0" for everything under a megawatt.
export const formatPowerKwOrDash = (powerKw: number | null | undefined): string => {
  if (powerKw === null || powerKw === undefined) return "—";
  return `${getDisplayValue(powerKw)} kW`;
};

// ---------------------------------------------------------------------------
// Efficiency display
// ---------------------------------------------------------------------------

export const formatEfficiency = (efficiencyJTh: number | null): string | null => {
  if (efficiencyJTh === null) return null;
  return `${separateByCommas(efficiencyJTh.toFixed(1))} J/TH`;
};

export const formatEfficiencyOrDash = (efficiencyJTh: number | null): string => formatEfficiency(efficiencyJTh) ?? "—";

// ---------------------------------------------------------------------------
// Temperature display
// ---------------------------------------------------------------------------

export const formatTempRange = (minC: number, maxC: number, temperatureUnit: TemperatureUnit): string => {
  const min = temperatureUnit === "F" ? convertCtoF(minC) : minC;
  const max = temperatureUnit === "F" ? convertCtoF(maxC) : maxC;
  return `${getDisplayValue(min)} °${temperatureUnit} – ${getDisplayValue(max)} °${temperatureUnit}`;
};

export const getAsicTempValue = (avgAsicTemp: number | undefined, isFahrenheit: boolean) => {
  if (!avgAsicTemp) return "N/A"; // TODO: why not return undefined, so we can show skeleton, also 0 cound be falsey
  return isFahrenheit ? convertCtoF(avgAsicTemp) : avgAsicTemp;
};
