// Display formatters for telemetry aggregates (hashrate, efficiency,
// power). Centralised so every surface — /sites metric row,
// /buildings/:id metric row, BuildingCard footer, future rack /
// device-set rollups — renders the same precision + unit ladder.
//
// Re-exports the lower-level `formatHashrateWithUnit` / `separateByCommas`
// already in shared/utils so callers compose these (display strings)
// rather than re-inventing the parts.

import { separateByCommas } from "@/shared/utils/stringUtils";
import { formatHashrateWithUnit } from "@/shared/utils/utility";

export const KW_PER_MW = 1_000;

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

export const formatEfficiency = (efficiencyJTh: number | null): string | null => {
  if (efficiencyJTh === null) return null;
  return `${separateByCommas(efficiencyJTh.toFixed(1))} J/TH`;
};

// "OrDash" variants for compact tiles (BuildingCard footer) that always
// need a non-null string and use em-dash as their no-data sentinel.
// Surfaces that disambiguate null vs value (e.g. the shared `Metric`
// primitive showing a skeleton) should keep using the null-returning
// versions above.
export const formatHashrateOrDash = (hashrateTh: number | null): string => formatHashrate(hashrateTh) ?? "—";

export const formatEfficiencyOrDash = (efficiencyJTh: number | null): string => formatEfficiency(efficiencyJTh) ?? "—";

// Total power as MW with em-dash fallback. Site/building rows use
// `formatPowerUsedCapacity` for used/capacity strings; this single-value
// variant is for footers that don't carry a capacity.
export const formatPowerMwOrDash = (powerKw: number | null): string => {
  if (powerKw === null) return "—";
  const mw = powerKw / KW_PER_MW;
  return `${separateByCommas(mw.toFixed(1))} MW`;
};
