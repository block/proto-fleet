import { FleetDuration } from "@/shared/components/DurationSelector";

export type Value = number | null;

export type AggregateStats = {
  avg?: Value;
  max?: Value;
  min?: Value;
};

export type TimeSeriesDataPoint = {
  datetime: number;
  value: Value;
};

export type StatsArgs = AggregateStats & { lowestPerformer?: string };

/**
 * ProtoFleet specific outlet context for KPI data
 */
export interface KpiOutletContext {
  duration: FleetDuration;
  minerHashrate: {
    hashrate: TimeSeriesDataPoint[];
    aggregates: AggregateStats;
  };
  minerEfficiency: {
    efficiency: TimeSeriesDataPoint[];
    aggregates: AggregateStats;
  };
  minerPowerUsage: {
    powerUsage: TimeSeriesDataPoint[];
    aggregates: AggregateStats;
  };
  minerTemperature: {
    temperature: TimeSeriesDataPoint[];
    aggregates: AggregateStats;
  };
}
