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

export type MetricTelemetry = {
  timeSeries?: MetricTimeSeries;
  latest?: Measurement;
};

// Telemetry Types
export interface MinerTelemetryData {
  hashboards: string[]; // hashboard ids
  hashrate?: MetricTelemetry;
  temperature?: MetricTelemetry;
  power?: MetricTelemetry;
  efficiency?: MetricTelemetry;
}

export interface HashboardTelemetryData {
  serial: string;
  inletTemp?: MetricTelemetry;
  outletTemp?: MetricTelemetry;
  avgAsicTemp?: MetricTelemetry;
  maxAsicTemp?: MetricTelemetry;
  hashrate?: MetricTelemetry;
  temperature?: MetricTelemetry;
  power?: MetricTelemetry;
  efficiency?: MetricTelemetry;
}

export interface AsicTelemetryData {
  id: string;
  hashrate?: MetricTelemetry;
  temperature?: MetricTelemetry;
}

// Chart data transformation types
export interface ChartDataPoint {
  datetime: number;
  [key: string]: number; // Dynamic keys for hashboard_1, hashboard_2, miner, etc.
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

  // TODO: [STORE_REFACTOR] these should be required but currently we populate hardware slice
  // from multiple APIs that provide different subsets of data
  // - useTelemetry provides: index, hashboardIndex
  // - useHashboardStatus provides: row, column
  row?: number;
  column?: number;
  index?: number;
  hashboardIndex?: number;
}

export type HashboardMap = Map<string, HashboardHardwareData>;
export type AsicMap = Map<string, AsicHardwareData>;

// Data Types (combining hardware + telemetry)
export type AsicData = AsicHardwareData & AsicTelemetryData;

export type HashboardData = HashboardHardwareData & HashboardTelemetryData;

export type MinerData = MinerHardwareData & MinerTelemetryData;
