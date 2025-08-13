import { Duration } from "@/shared/components/DurationSelector";
import {
  AggregateStats,
  TimeSeriesDataPoint,
} from "@/shared/features/kpis/types";

/**
 * ProtoFleet specific outlet context for KPI data
 */
export interface KpiOutletContext {
  duration: Duration;
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
