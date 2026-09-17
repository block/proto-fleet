// =============================================================================
// Shared Types for Miner Store
// =============================================================================

// TODO: [STORE_REFACTOR] Eventually would like to move these types to /shared/types so that they can used by ProtoOS and ProtoFleet
// Whatever the generated types are from either API, we would convert them to these shared types before storing in Zustand
// THis would let us use shared utilities for things like unit conversion, formatting, etc. which then our shared/components could use
// Currently we do this separately on each client before passing built-in types to shared components.
// This could also help with rendering single miner views in ProtoFleet.

import type {
  AsicTelemetry,
  MetricUnit as ContractMetricUnit,
  FanTelemetry,
  HashboardTelemetry,
  LevelRollup,
  PsuTelemetry,
} from "./contract";
import type { FlowUnit, TemperatureUnit, Theme, ThemeColor } from "@/shared/features/preferences/types";

// reexporting types defined in shared.  Eventually all of these types should be defined in shared
// and we wont need to do any reexporting
export { Theme, ThemeColor, TemperatureUnit, FlowUnit };

type Value = number | null;

// Display units = the canonical contract unit vocabulary (`contract.MetricUnit`)
// plus the alternate/derived units we only ever convert *to* for display and never
// store: Fahrenheit, kW/MW, mV/mA, GHz. Because this is a strict superset of the
// contract units, a measurement read straight off the contract is assignable here
// without adaptation. The Celsius/Fahrenheit *preference* is a separate semantic
// enum (`TemperatureUnit`, "C" | "F") mapped to "°C" | "°F" at the render boundary
// via `temperatureDisplayUnit`.
export type DisplayMetricUnit = ContractMetricUnit | "°F" | "kW" | "MW" | "mV" | "mA" | "GHz";

// Display measurement: the value/units pair consumed by formatting + unit-conversion
// helpers (`telemetryUtils`) and the value components. Same shape as the store's
// canonical `contract.Measurement`, but its `units` may also be a post-conversion
// display unit ("kW", "°F", ...).
export type Measurement = {
  value: Value;
  units: DisplayMetricUnit | undefined;
  formatted?: string;
};

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

// Data Types (combining hardware + normalized-contract telemetry). The telemetry
// half is the contract entity type minus its id key (the hardware half already
// carries the identifier), so consumers read the normalized contract shape directly.
export type AsicData = AsicHardwareData & Omit<AsicTelemetry, "asicId">;

export type HashboardData = HashboardHardwareData & Omit<HashboardTelemetry, "hashboardId">;

export type MinerData = MinerHardwareData & Pick<LevelRollup, "hashrate" | "power" | "efficiency" | "temperature">;

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

export type PsuMap = Map<number, PsuHardwareData>;
export type PsuData = PsuHardwareData & Omit<PsuTelemetry, "componentId">;

// Fan Types
export interface FanHardwareData {
  slot: number; // physical slot number (1-based from API)
  name?: string;
}

export type FanMap = Map<number, FanHardwareData>;
export type FanData = FanHardwareData & Omit<FanTelemetry, "componentId">;

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
