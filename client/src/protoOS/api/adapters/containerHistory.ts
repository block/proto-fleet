import type { MetricTimeSeries } from "@/protoOS/store/contract";

export type ContainerHistoryMetric =
  | "hashrate"
  | "power"
  | "temperature"
  | "plantPower"
  | "facilityPower"
  | "heatRejectionPower"
  | "insideTemperature"
  | "outsideTemperature"
  | "inletTemperature"
  | "outletTemperature"
  | "outletATemperature"
  | "outletBTemperature"
  | "distributionPower"
  | "fanPower";

export type ContainerHistorySelection =
  | { metric: "distributionPower" | "fanPower"; identifier: string }
  | { metric: Exclude<ContainerHistoryMetric, "distributionPower" | "fanPower">; identifier?: never };

export interface ContainerHistoryRequest {
  startTime: string;
  endTime: string;
  intervalMs: number;
  selections: ContainerHistorySelection[];
}

export interface ContainerHistoryResult {
  series: Partial<Record<string, MetricTimeSeries>>;
  /** A failed metric must not hide successful siblings or become a zero series. */
  unavailable: string[];
}

export const containerHistoryKey = (selection: ContainerHistorySelection) =>
  `${selection.metric}:${selection.identifier ?? ""}`;
