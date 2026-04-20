// =============================================================================
// Shared Types for Miner Store
// =============================================================================

// TODO: [STORE_REFACTOR] Eventually would like to move these types to /shared/types so that they can used by ProtoOS and ProtoFleet
// Whatever the generated types are from either API, we would convert them to these shared types before storing in Zustand
// THis would let us use shared utilities for things like unit conversion, formatting, etc. which then our shared/components could use
// Currently we do this separately on each client before passing built-in types to shared components.
// This could also help with rendering single miner views in ProtoFleet.

import type { TemperatureUnit, Theme, ThemeColor } from "@/shared/features/preferences/types";

// reexporting types defined in shared.  Eventually all of these types should be defined in shared
// and we wont need to do any reexporting
export { Theme, ThemeColor, TemperatureUnit };

export type PowerUnit = "W" | "kW" | "MW";
export type HashrateUnit = "TH/s" | "TH/S" | "GH/s" | "GH/S" | "MH/s" | "MH/S";
export type EfficiencyUnit = "J/TH";
export type PercentageUnit = "%";
export type RpmUnit = "RPM";
export type VoltageUnit = "V" | "mV";
export type CurrentUnit = "A" | "mA";
export type FrequencyUnit = "MHz" | "GHz";

type Value = number | null;

export type MetricUnit =
  | TemperatureUnit
  | PowerUnit
  | HashrateUnit
  | EfficiencyUnit
  | PercentageUnit
  | RpmUnit
  | VoltageUnit
  | CurrentUnit
  | FrequencyUnit;

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
  voltage?: MetricTelemetry;
  frequency?: MetricTelemetry;
}

// Chart data transformation types
export interface ChartDataPoint {
  datetime: number;
  [key: string]: number; // Dynamic keys for hashboard_1, hashboard_2, miner, etc.
}

// Hardware Types
export interface ControlBoardHardwareData {
  serial?: string;
  boardId?: string;
  machineName?: string;
  firmware?: {
    name?: string;
    version?: string;
    variant?: string;
    gitHash?: string;
    imageHash?: string;
  };
  mpu?: {
    cpuArchitecture?: number;
    cpuImplementer?: string;
    cpuPart?: string;
    cpuRevision?: number;
    cpuVariant?: string;
    hardware?: string;
    modelName?: string;
    processor?: number;
    revision?: string;
  };
}

export interface MinerHardwareData {
  controlBoardSerial?: string;
  hashboardSerials: string[];
}

export interface HashboardHardwareData {
  serial: string;
  slot?: number;
  bay?: number;
  board?: string;
  asicIds?: string[];

  // Additional fields from HashboardInfo API
  apiVersion?: string;
  chipId?: string;
  port?: number;
  miningAsic?: "BZM" | "MC1" | "MC2" | "MC3";
  miningAsicCount?: number;
  temperatureSensorCount?: number;
  ecLogsPath?: string;
  firmware?: {
    version?: string;
    build?: "debug" | "release";
    gitHash?: string;
    imageHash?: string;
  };
  bootloader?: {
    version?: string;
    build?: "debug" | "release";
    gitHash?: string;
    imageHash?: string;
  };
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

// PSU Types
export interface PsuHardwareData {
  id: number; // unique identifier (1-3 for slots)
  serial?: string;
  slot?: number;
  manufacturer?: string;
  model?: string;
  hwRevision?: string;
  firmware?: {
    appVersion?: string;
    bootloaderVersion?: string;
  };
}

export interface PsuTelemetryData {
  id: number;
  inputVoltage?: MetricTelemetry;
  inputCurrent?: MetricTelemetry;
  inputPower?: MetricTelemetry;
  outputVoltage?: MetricTelemetry;
  outputCurrent?: MetricTelemetry;
  outputPower?: MetricTelemetry;
  temperatureAmbient?: MetricTelemetry;
  temperatureAverage?: MetricTelemetry;
  temperatureHotspot?: MetricTelemetry;
}

export type PsuMap = Map<number, PsuHardwareData>;
export type PsuData = PsuHardwareData & PsuTelemetryData;

// Fan Types
export interface FanHardwareData {
  slot: number; // physical slot number (1-based from API)
  name?: string;
}

export interface FanTelemetryData {
  slot: number; // matches FanHardwareData.slot
  rpm?: MetricTelemetry;
  percentage?: MetricTelemetry;
  minRpm?: MetricTelemetry;
  maxRpm?: MetricTelemetry;
}

export type FanMap = Map<number, FanHardwareData>;
export type FanData = FanHardwareData & FanTelemetryData;

// Error Types
export type ErrorSource = "RIG" | "FAN" | "PSU" | "HASHBOARD";

export interface MinerError {
  errorCode: string; // Maps from error_code in API
  timestamp?: number; // Unix timestamp from API (optional)
  source: ErrorSource; // Maps directly from API source
  slot?: number; // Component slot from API (1-based, optional)
  message: string; // Error message from API
}

// Auth Types
export const AUTH_ACTIONS = {
  sleep: "sleep",
  wake: "wake",
  reboot: "reboot",
  update: "update",
  miningTarget: "miningTarget",
  locate: "locate",
  systemTag: "systemTag",
} as const;

export type AuthAction = keyof typeof AUTH_ACTIONS | null;
