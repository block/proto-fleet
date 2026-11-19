import type { ModuleStatus } from "@/protoOS/features/tanks/tankView";

export type HomeTab = "overview" | "hashrate" | "efficiency" | "power" | "temperature" | "system";
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

export interface ModuleGridCell {
  id: string;
  /** One-based column; cells without a reported slot follow in topology order. */
  column: number;
  name: string;
  to: string;
  state: ModuleStatus;
  /** Hashboard states in slot order. */
  boards: ModuleStatus[];
}

export interface ModuleGrid {
  columns: number;
  tanks: { id: string; label: string; modules: ModuleGridCell[] }[];
  counts: Record<ModuleStatus, number>;
}

export interface IssueGroup {
  id: string;
  label: string;
  severity: "critical" | "warning";
  /** What the group affects, such as "154 boards". */
  affected: string;
}

export interface IssueSummary {
  critical: number;
  warning: number;
  groups: IssueGroup[];
}
