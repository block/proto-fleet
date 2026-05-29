// Aggregated site / building metrics ship out of the API in the same units
// individual miner snapshots use (TH/s, kW, J/TH). These helpers format them
// for the operational headers on /sites and /buildings/:id.
//
// Single source of truth: SiteMetricsRow, BuildingMetricsRow, and
// BuildingCard's footer all consume the helpers below so the precision +
// unit ladder stays consistent across surfaces.

import { separateByCommas } from "@/shared/utils/stringUtils";
import { formatHashrateWithUnit } from "@/shared/utils/utility";

export const KW_PER_MW = 1_000;

export const formatLocation = (city: string, state: string): string | null => {
  const c = city.trim();
  const s = state.trim();
  if (c && s) return `${c}, ${s}`;
  if (c) return c;
  if (s) return s;
  return null;
};

// Picks GH/s, TH/s, PH/s, or EH/s based on the input so the displayed value
// stays in [1, 1000) — small sites (a single test miner at 400 TH/s) don't
// read as "0.00 EH/s", and EH-scale fleets don't read as "1,250,000 TH/s".
export const formatHashrate = (hashrateTh: number | null): string | null => {
  if (hashrateTh === null) return null;
  if (hashrateTh === 0) return "0 TH/s";
  const { value, unit } = formatHashrateWithUnit(hashrateTh);
  // Two decimals when scaled value is < 10 so small magnitudes keep signal
  // (e.g. 2.50 EH/s); one decimal above that to match the dashboard bar.
  const decimals = value < 10 ? 2 : 1;
  // Shared formatter returns uppercase units (PH/S); the metric row uses
  // lowercase /s throughout.
  return `${separateByCommas(value.toFixed(decimals))} ${unit.replace("/S", "/s")}`;
};

export const formatPowerUsedCapacity = (usedKw: number | null, capacityMw: number): string | null => {
  const hasCapacity = capacityMw > 0;
  if (usedKw === null && !hasCapacity) return null;
  const usedMw = usedKw !== null ? usedKw / KW_PER_MW : null;
  const usedText = usedMw !== null ? usedMw.toFixed(1) : "—";
  const capacityText = hasCapacity ? capacityMw.toFixed(1) : "—";
  return `${usedText} / ${capacityText} MW`;
};

export const formatEfficiency = (efficiencyJTh: number | null): string | null => {
  if (efficiencyJTh === null) return null;
  return `${separateByCommas(efficiencyJTh.toFixed(1))} J/TH`;
};

// Used by BuildingCard's compact footer: same auto-scaled hashrate as
// `formatHashrate` but always returns a non-null string and falls back to
// "—" when the input is null. Callers that already disambiguate null vs
// value can continue using `formatHashrate` directly.
export const formatHashrateOrDash = (hashrateTh: number | null): string => formatHashrate(hashrateTh) ?? "—";

export const formatEfficiencyOrDash = (efficiencyJTh: number | null): string => formatEfficiency(efficiencyJTh) ?? "—";

// Total power as MW, with em-dash fallback when unset. The
// site/building rows use `formatPowerUsedCapacity` for "used / capacity"
// strings; the BuildingCard footer renders only the used value.
export const formatPowerMwOrDash = (powerKw: number | null): string => {
  if (powerKw === null) return "—";
  const mw = powerKw / KW_PER_MW;
  return `${separateByCommas(mw.toFixed(1))} MW`;
};
