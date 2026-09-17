export type HomeTab = "overview" | "hashrate" | "efficiency" | "power" | "temperature";
export type HomeChartMode = "system" | "hashboards" | "fluid";

export interface HomeReading {
  id: string;
  label: string;
  value: string;
}

export interface HomeSeries {
  id: string;
  label: string;
  color: string;
  latest: string;
  points: { datetime: number; value: number | null }[];
  high?: string;
  low?: string;
  average?: string;
}

export interface HomeHistory {
  series: HomeSeries[];
  units: string;
  window: [number, number];
  loading?: boolean;
  error?: string;
  /** Only disjoint, fully attributed power series may be stacked. */
  stacked?: boolean;
}

export interface LocationReading extends HomeReading {
  /** Null means unavailable, not an empty tank or zero power. */
  amount: number | null;
}

export interface CategorySummary {
  id: string;
  label: string;
  high: string;
  low: string;
  average: string;
}
