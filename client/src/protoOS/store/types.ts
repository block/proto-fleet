// =============================================================================
// Shared Types for Miner Store
// =============================================================================

// TODO: [STORE_REFACTOR] Eventually would like to move these types to /shared/types so that they can used by ProtoOS and ProtoFleet
// Whatever the generated types are from either API, we would convert them to these shared types before storing in Zustand
// THis would let us use shared utilities for things like unit conversion, formatting, etc. which then our shared/components could use
// Currently we do this separately on each client before passing built-in types to shared components.
// This could also help with rendering single miner views in ProtoFleet.

// TODO: [STORE_REFACTOR] ideally these would come from generated API types but currently unit is just string there
export type TemperatureUnit = "C" | "F";
export type PowerUnit = "W" | "kW" | "MW";
export type HashrateUnit = "TH/s" | "TH/S" | "GH/s" | "GH/S" | "MH/s" | "MH/S";
export type EfficiencyUnit = "J/TH";
export type PercentageUnit = "%";

type Value = number | null;

export type MetricUnit =
  | TemperatureUnit
  | PowerUnit
  | HashrateUnit
  | EfficiencyUnit
  | PercentageUnit;

// Time Series Data Types
export interface MetricTimeSeries {
  aggregates?: {
    min?: Measurement;
    avg?: Measurement;
    max?: Measurement;
  };
  units: MetricUnit;
  values: Value[];
  startTime: number;
  endTime: number;
}

export type Measurement = {
  value: Value;
  units: MetricUnit | undefined;
  formatted?: string;
};

// TODO : [STORE_REFACTOR] Now that Telemetry API for getting latest values is in flight we should rethink this interface expose
// latest values as well as time series values (last time series value is an average over an interval so they dont match latest value)
// either current value counld be a child of MetricTimeSeris or we could have the time series and latest values be siblings
export interface Telemetry {
  hashrate?: MetricTimeSeries;
  temperature?: MetricTimeSeries;
  power?: MetricTimeSeries;
  efficiency?: MetricTimeSeries;
}

// Hardware Types
export interface MinerHardwareData {
  controlBoardSerial?: string;
  hashboardSerials: string[];
}

export interface HashboardHardwareData {
  serial: string;
  slot?: number;
  bay?: number;
  board?: string;
  slotIndexByBay?: number;
  asicIds?: string[];
}

export interface AsicHardwareData {
  id: string;
  hashboardSerial: string;
  row: number;
  column: number;

  // TODO: [STORE_REFACTOR] these should be required but currently we populate part of hardware slice
  // from older APIs that do not provide index or hashboardIndex
  index?: number;
  hashboardIndex?: number;
}

export type HashboardMap = Map<string, HashboardHardwareData>;
export type AsicMap = Map<string, AsicHardwareData>;

// Telemetry Entity Types
export interface MinerTelemetryData extends Telemetry {
  controlBoardSerial: string;
  hashboards: string[]; // hashboard ids
}

export interface HashboardTelemetryData extends Telemetry {
  serial: string;
  inletTemp?: Measurement;
  outletTemp?: Measurement;
  avgAsicTemp?: Measurement;
  maxAsicTemp?: Measurement;
}

export interface AsicTelemetryData extends Telemetry {
  id: string;
}

// Chart data transformation types
export interface ChartDataPoint {
  datetime: number;
  [key: string]: number; // Dynamic keys for hashboard_1, hashboard_2, miner, etc.
}

// Data Types (combining hardware + telemetry)
export type AsicData = AsicHardwareData & AsicTelemetryData;

export type HashboardData = HashboardHardwareData & HashboardTelemetryData;

export type MinerData = MinerHardwareData & Telemetry;
