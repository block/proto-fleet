/** Shared by rack/miner and tank/module cards. */
export type HeatBand = 0 | 1 | 2 | 3 | 4 | 5;
export const healthHeatBand = (ratio: number): HeatBand => {
  if (ratio <= 0) return 0;
  if (ratio < 0.05) return 1;
  if (ratio < 0.15) return 2;
  if (ratio < 0.3) return 3;
  if (ratio < 0.5) return 4;
  return 5;
};
export const HEAT_CLASS: Record<HeatBand, string> = {
  0: "bg-core-primary-fill/10",
  1: "bg-intent-critical-fill/8",
  2: "bg-intent-critical-fill/18",
  3: "bg-intent-critical-fill/32",
  4: "bg-intent-critical-fill/50",
  5: "bg-intent-critical-fill/72",
};
