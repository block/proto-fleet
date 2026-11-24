import { moduleStatuses } from "@/protoOS/features/tanks/tankView";

export const overviewModuleStatuses = moduleStatuses.filter((status) => status !== "isolated");
export type OverviewModuleStatus = (typeof overviewModuleStatuses)[number];

export type HomeTab = "overview" | "hashrate" | "efficiency" | "power" | "temperature" | "system";
export type HomeChartMode = "system" | "fluid";

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
  label: string;
  position?: { row: number; column: number };
  name: string;
  to: string;
  state: OverviewModuleStatus;
  /** Hashboard states in slot order. */
  boards: (OverviewModuleStatus | "warning" | "empty")[];
  attentionSeverity?: "critical" | "warning";
}

export interface ModuleGrid {
  tanks: { id: string; label: string; slot?: number; columns: number; rows: number; modules: ModuleGridCell[] }[];
  counts: Record<OverviewModuleStatus, number>;
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
