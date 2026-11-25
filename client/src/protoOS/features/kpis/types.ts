import { Duration } from "@/shared/components/DurationSelector";

export interface TimeSeriesData {
  datetime?: number;
  value?: number | null;
}

export interface Aggregates {
  avg?: number | null;
  max?: number | null;
  min?: number;
}

export type KpiOutletContext = {
  duration: Duration;
  hashboardSerials: string[];
  minerHashrate: {
    hashrate: TimeSeriesData[];
    aggregates: Aggregates;
  };
  minerEfficiency: {
    efficiency: TimeSeriesData[];
    aggregates: Aggregates;
  };
  minerPowerUsage: {
    powerUsage: TimeSeriesData[];
    aggregates: Aggregates;
  };
  minerTemperature: {
    temperature: TimeSeriesData[];
    aggregates: Aggregates;
  };
};
