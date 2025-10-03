import { Aggregates, TimeSeriesData } from "@/protoOS/api/generatedApi";
import { Duration } from "@/shared/components/DurationSelector";

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
