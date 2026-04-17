/* eslint-disable */
/* tslint:disable */
// @ts-nocheck
/*
 * ---------------------------------------------------------------
 * ## THIS FILE WAS GENERATED VIA SWAGGER-TYPESCRIPT-API        ##
 * ##                                                           ##
 * ## AUTHOR: acacode                                           ##
 * ## SOURCE: https://github.com/acacode/swagger-typescript-api ##
 * ---------------------------------------------------------------
 */

/** Statistical aggregates for time series data */
export interface Aggregates {
  /**
   * Average value in data.
   * @example 50.5
   */
  avg?: number | null;
  /**
   * Maximum value in data.
   * @example 100.5
   */
  max?: number | null;
  /**
   * Minimum value in data.
   * @example 0.5
   */
  min?: number;
}

/** Available field types for ASIC-level data */
export enum AsicFieldType {
  Hashrate = "hashrate",
  Temperature = "temperature",
}

/** Statistics and performance data for an individual ASIC chip */
export interface AsicStats {
  /**
   * Physical column location of the ASIC on the hashboard.
   * @example 10
   */
  column?: number;
  /**
   * The number of times that the ASIC produced an incorrect hash or an error during a specific period of time.  Error Rate (%) = (Number of incorrect hash / Total number of expected Hash) x 100%
   * @example 3.3
   */
  error_rate?: number;
  /**
   * The frequency of the ASIC measured in megahertz.
   * @example 650.05
   */
  freq_mhz?: number;
  /**
   * The current hashrate of the ASIC, measured in GH/s.
   * @example 300.05
   */
  hashrate_ghs?: number;
  /**
   * Human-readable ASIC identifier (e.g. "A0", "B3").
   * @example "A0"
   */
  id?: string;
  /**
   * The expected hashrate determined by the clock frequency of the ASIC, measured in GH/s.
   * @example 300.05
   */
  ideal_hashrate_ghs?: number;
  /**
   * Zero-based index of the ASIC on the hashboard (first ASIC is 0).
   * @example 0
   */
  index?: number;
  /**
   * Physical row location of the ASIC on the hashboard.
   * @example 0
   */
  row?: number;
  /**
   * Current temperature of the ASIC in celsius
   * @example 45.5
   */
  temp_c?: number;
  /**
   * The present voltage being supplied to the ASIC in millivolts.
   * @example 308.44
   */
  voltage_mv?: number;
}

/** Response containing statistics data for a specific ASIC chip */
export interface AsicStatsResponse {
  /** Statistics and performance data for an individual ASIC chip */
  "asic-stats"?: AsicStats;
}

/** ASIC-level telemetry metrics */
export interface AsicTelemetry {
  /** An array of metric values with a shared unit */
  hashrate: MetricArray;
  /** An array of metric values with a shared unit */
  temperature: MetricArray;
}

/** JWT authentication tokens for access and refresh operations */
export interface AuthTokens {
  /**
   * JWT access token.
   * @example "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
   */
  access_token: string;
  /**
   * JWT refresh token.
   * @example "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
   */
  refresh_token: string;
}

/** Information about an available PSU firmware file */
export interface AvailablePsuFirmware {
  /**
   * SHA-256 hash of the firmware file
   * @example "b5f8a2f4e5c19ac7e2e4d0a87f3a1a6b6e0c8b991f2e99a4f5cd5e512a5f4d1e3"
   */
  sha256: string;
  /**
   * Firmware file name
   * @example "chicony-s24-1.18.22.bin"
   */
  filename: string;
  /**
   * Firmware version string
   * @example "1.18.22"
   */
  firmware_version: string;
  /**
   * Compatible PSU model
   * @example "S24"
   */
  model: string;
  /**
   * File size in bytes
   * @format int64
   * @example 1048576
   */
  size_bytes: number;
}

/** Request data for changing a user's password with current password verification */
export interface ChangePasswordRequest {
  /**
   * The current password for authentication
   * @format password
   * @minLength 8
   * @example "current_password123"
   */
  current_password: string;
  /**
   * The new password to set
   * @format password
   * @minLength 8
   * @example "new_password456"
   */
  new_password: string;
}

/** Complete control board hardware and firmware information */
export interface ControlBoardInfo {
  /**
   * Board ID identifier.
   * @example "8"
   */
  board_id?: string;
  /** Control board Linux firmware information */
  firmware?: ControlBoardInfoControlboardlinuxasset;
  /**
   * Machine name identifier.
   * @example "c3-p1"
   */
  machine_name?: string;
  /** CPU and processor information from the control board */
  mpu?: ControlBoardInfoMpuinfo;
  /**
   * Control board serial number.
   * @example "515CP79107000107"
   */
  serial_number?: string;
}

/** Control board Linux firmware information */
export interface ControlBoardInfoControlboardlinuxasset {
  /**
   * Git commit hash.
   * @example "c5e0a8aed4ee73fc9e04d918909dac43f3d5502a"
   */
  git_hash?: string;
  /**
   * Image hash.
   * @example "unknown"
   */
  image_hash?: string;
  /**
   * Firmware name.
   * @example "Proto Embedded Linux Distribution"
   */
  name?: string;
  /**
   * Firmware variant.
   * @example "mfg"
   */
  variant?: string;
  /**
   * Firmware version.
   * @example "0.1.49"
   */
  version?: string;
}

/** CPU and processor information from the control board */
export interface ControlBoardInfoMpuinfo {
  /**
   * CPU architecture version.
   * @example 7
   */
  cpu_architecture?: number;
  /**
   * CPU implementer identifier.
   * @example "0x41"
   */
  cpu_implementer?: string;
  /**
   * CPU part identifier.
   * @example "0xc07"
   */
  cpu_part?: string;
  /**
   * CPU revision number.
   * @example 5
   */
  cpu_revision?: number;
  /**
   * CPU variant identifier.
   * @example "0x0"
   */
  cpu_variant?: string;
  /**
   * Hardware platform identifier.
   * @example "STM32 (Device Tree Support)"
   */
  hardware?: string;
  /**
   * CPU model name.
   * @example "ARMv7 Processor rev 5 (v7l)"
   */
  model_name?: string;
  /**
   * Processor number.
   * @example 0
   */
  processor?: number;
  /**
   * Board revision identifier.
   * @example "0000"
   */
  revision?: string;
  /**
   * Board serial number.
   * @example "001B00353133510635303638"
   */
  serial?: string;
}

/** Cooling system configuration for fan control modes */
export interface CoolingConfig {
  /**
   * Parameter to define the cooling mode.  Modes:
   *  - Unknown: Cooling mode is not yet determined.
   *  - Off: Fans will be set to off for immersion cooling.
   *  - Auto: Fans will be controlled based on miner temperature.
   *  - Manual: Fans run at a specified percentage of their maximum speed.
   * @example "Auto"
   */
  mode?: "Unknown" | "Off" | "Auto" | "Manual";
  /**
   * Fan speed as a percentage of maximum RPM (valid range: 0-100). Used only when mode is Manual.
   * @min 0
   * @max 100
   * @example 75
   */
  speed_percentage?: number;
  /**
   * Target temperature in Celsius for automatic fan control. Used only when mode is Auto to override the default temperature target.
   * @format float
   * @example 50
   */
  target_temperature_c?: number;
}

/** Current cooling system status and fan information */
export interface CoolingStatus {
  /** Cooling system status and performance information */
  "cooling-status"?: CoolingStatusCoolingstatus;
}

/** Cooling system status and performance information */
export interface CoolingStatusCoolingstatus {
  /**
   * Current fan control mode.
   *  - Unknown: Cooling mode is not yet determined.
   *  - Off: Fans are disabled.
   *  - Auto: Fans are controlled automatically based on temperature.
   *  - Manual: Fans are set to a fixed speed percentage.
   * @example "Auto"
   */
  fan_mode?: "Unknown" | "Off" | "Auto" | "Manual";
  /** This will show speed of all fans in the system. */
  fans?: FanStatus[];
  /**
   * Current effective fan speed percentage (0-100). Relevant when mode is Manual.
   * @min 0
   * @max 100
   * @example 55
   */
  speed_percentage?: number;
  /**
   * Current target temperature in Celsius for automatic fan control. Only present when mode is Auto.
   * @format float
   * @example 50
   */
  target_temperature_c?: number;
}

export interface DeletePoolParams {
  /** The pool ID to delete */
  id: number;
}

export interface EditPoolParams {
  /** The pool ID to update */
  id: number;
}

/** Response containing historical mining efficiency data over time */
export interface EfficiencyResponse {
  /** Efficiency data response with time series information */
  "efficiency-data"?: EfficiencyResponseEfficiencydata;
}

/** Efficiency data response with time series information */
export interface EfficiencyResponseEfficiencydata {
  /** Statistical aggregates for time series data */
  aggregates?: Aggregates;
  data?: TimeSeriesData[];
  /** Duration of time series data returned. */
  duration?: TimeSeriesDuration;
}

/** Error information with code and message details */
export interface Error {
  /**
   * Error code.
   * @example "INCORRECT_ARGS"
   */
  code?: string;
  /**
   * Error message.
   * @example "Arguments are incorrect for query."
   */
  message?: string;
}

/** Array of notification errors for system error reporting */
export type ErrorListResponse = NotificationError[];

/** Error response containing error details */
export interface ErrorResponse {
  /** Error information with code and message details */
  error?: Error;
}

/** Firmware version and build information */
export interface FWInfo {
  /** @example "release" */
  build?: "debug" | "release";
  /** @example "1213423223" */
  git_hash?: string;
  /** @example "1213423223" */
  image_hash?: string;
  /** @example "1.0" */
  version?: string;
}

/** Individual fan information including status and RPM data */
export interface FanInfo {
  /**
   * The maximum RPM of the cooling device.
   * @example 1000
   */
  max_rpm?: number | null;
  /**
   * The minimum RPM of the cooling device.
   * @example 1000
   */
  min_rpm?: number | null;
  /**
   * The name of the cooling device.
   * @example "CPU Cooler"
   */
  name?: string;
  /**
   * Each cooling device is assigned a unique identifier starting from 1.
   * @example 1
   */
  slot?: number;
}

/** Current status and performance metrics for individual cooling fans */
export interface FanStatus {
  /**
   * The fan's set speed as a percentage from 0 to 100.
   * @example 55
   */
  percentage?: number;
  /**
   * The fan's current rotations per minute (RPM).
   * @example 1200
   */
  rpm?: number;
  /**
   * Each fan is assigned a unique identifier starting from 1.
   * @example 1
   */
  slot?: number;
}

export interface GetAsicHashrateParams {
  /** The ID of the ASIC to provide hashrate information for. */
  asicId: number;
  /**
   * Time duration for historical data retrieval. Defaults to 1h if not specified.
   * @default "1h"
   */
  duration?: "1h" | "12h" | "24h" | "48h" | "5d";
  /**
   * Time interval granularity for data points. Defaults to 1m if not specified.
   * @default "1m"
   */
  granularity?: "1m" | "5m" | "15m";
  /**
   * The serial number of the hashboard.
   * @example "YWWLMMMMRRFSSSSS"
   */
  hbSn: string;
}

export interface GetAsicStatusParams {
  /** The id of an ASIC to provide statistics for. */
  asicId: number;
  /**
   * The serial number of the hashboard.
   * @example "YWWLMMMMRRFSSSSS"
   */
  hbSn: string;
}

export interface GetAsicTemperatureParams {
  /** The ID of the ASIC to provide temperature information for. */
  asicId: number;
  /**
   * Time duration for historical data retrieval. Defaults to 1h if not specified.
   * @default "1h"
   */
  duration?: "1h" | "12h" | "24h" | "48h" | "5d";
  /**
   * Time interval granularity for data points. Defaults to 1m if not specified.
   * @default "1m"
   */
  granularity?: "1m" | "5m" | "15m";
  /**
   * The serial number of the hashboard.
   * @example "YWWLMMMMRRFSSSSS"
   */
  hbSn: string;
}

export interface GetCurrentTelemetryParams {
  /** Data types to include in response. Defaults to 'miner' if not specified. */
  level?: ("miner" | "hashboard" | "psu" | "asic")[];
}

export interface GetHashboardEfficiencyParams {
  /**
   * Time duration for historical data retrieval. Defaults to 1h if not specified.
   * @default "1h"
   */
  duration?: "1h" | "12h" | "24h" | "48h" | "5d";
  /**
   * The serial number of the hashboard.
   * @example "YWWLMMMMRRFSSSSS"
   */
  hbSn: string;
}

export interface GetHashboardHashrateParams {
  /**
   * Time duration for historical data retrieval. Defaults to 1h if not specified.
   * @default "1h"
   */
  duration?: "1h" | "12h" | "24h" | "48h" | "5d";
  /**
   * The serial number of the hashboard.
   * @example "YWWLMMMMRRFSSSSS"
   */
  hbSn: string;
}

export interface GetHashboardPowerParams {
  /**
   * Time duration for historical data retrieval. Defaults to 1h if not specified.
   * @default "1h"
   */
  duration?: "1h" | "12h" | "24h" | "48h" | "5d";
  /**
   * The serial number of the hashboard.
   * @example "YWWLMMMMRRFSSSSS"
   */
  hbSn: string;
}

export interface GetHashboardStatusParams {
  /**
   * The serial number of the hashboard.
   * @example "YWWLMMMMRRFSSSSS"
   */
  hbSn: string;
}

export interface GetHashboardTemperatureParams {
  /**
   * Time duration for historical data retrieval. Defaults to 1h if not specified.
   * @default "1h"
   */
  duration?: "1h" | "12h" | "24h" | "48h" | "5d";
  /**
   * The serial number of the hashboard.
   * @example "YWWLMMMMRRFSSSSS"
   */
  hbSn: string;
}

export interface GetMinerEfficiencyParams {
  /**
   * Time duration for historical data retrieval. Defaults to 1h if not specified.
   * @default "1h"
   */
  duration?: "1h" | "12h" | "24h" | "48h" | "5d";
}

export interface GetMinerHashrateParams {
  /**
   * Time duration for historical data retrieval. Defaults to 1h if not specified.
   * @default "1h"
   */
  duration?: "1h" | "12h" | "24h" | "48h" | "5d";
}

export interface GetMinerPowerParams {
  /**
   * Time duration for historical data retrieval. Defaults to 1h if not specified.
   * @default "1h"
   */
  duration?: "1h" | "12h" | "24h" | "48h" | "5d";
}

export interface GetMinerTemperatureParams {
  /**
   * Time duration for historical data retrieval. Defaults to 1h if not specified.
   * @default "1h"
   */
  duration?: "1h" | "12h" | "24h" | "48h" | "5d";
}

export interface GetPoolParams {
  /** The pool ID to retrieve */
  id: number;
}

export interface GetSystemLogsParams {
  /**
   * Number of log lines to return from the tail of the log, up to a maximum of 10000 lines. Defaults to 100 lines.
   * @default 100
   */
  lines?: number;
  /**
   * Source of logs to fetch. Defaults to miner software logs.
   * @default "miner_sw"
   * @example "miner_sw"
   */
  source?: "os" | "pool_sw" | "miner_sw" | "miner_web_server";
}

/** Complete hardware information including hashboards, PSUs, and cooling components */
export interface HardwareInfo {
  /** Hardware information and specifications */
  "hardware-info"?: HardwareInfoHardwareinfo;
}

/** Hardware information and specifications */
export interface HardwareInfoHardwareinfo {
  /** Complete control board hardware and firmware information */
  "cb-info"?: ControlBoardInfo;
  "fans-info"?: FanInfo[];
  "hashboards-info"?: HashboardInfo[];
  "psus-info"?: PsuInfo[];
}

/** Available field types for hashboard-level data */
export enum HashboardFieldType {
  Hashrate = "hashrate",
  Temperature = "temperature",
  InletTemp = "inletTemp",
  OutletTemp = "outletTemp",
  Power = "power",
  Efficiency = "efficiency",
}

/** Information about mining hashboards configuration and status */
export interface HashboardInfo {
  /** @example "1.0" */
  api_version?: string;
  /** @example "B3a" */
  board?: "CpuSimulated" | "B2" | "B3a" | "B3b" | "B3bSim" | "B4" | "B4Sim";
  /** Firmware version and build information */
  bootloader?: FWInfo;
  /** @example "ABC123" */
  chip_id?: string;
  /**
   * The absolute path where EC logs are stored.
   * @example "/var/log/ec_logs"
   */
  ec_logs_path?: string;
  /** Firmware version and build information */
  firmware?: FWInfo;
  /**
   * Hashboard serial number.
   * @example "YWWLMMMMRRFSSSSS"
   */
  hb_sn?: string;
  /** @example "BZM" */
  mining_asic?: "BZM" | "MC1" | "MC2" | "MC3";
  /**
   * Number of asics on the hashboard.
   * @example 100
   */
  mining_asic_count?: number;
  /**
   * The USB port number the hashboard is connected to.
   * @example 0
   */
  port?: number;
  /**
   * The physical slot where the hashboard is inserted in the system.
   * @example 1
   */
  slot?: number;
  /**
   * Number of temperature sensors on the hashboard.
   * @example 3
   */
  temp_sensor_count?: number;
}

/** Statistics and status information for a hashboard */
export interface HashboardStats {
  /** Hashboard performance statistics and metrics */
  "hashboard-stats"?: HashboardStatsHashboardstats;
}

/** Hashboard performance statistics and metrics */
export interface HashboardStatsHashboardstats {
  asics?: AsicStats[];
  /**
   * Current average temperature of the hashboard in celsius.
   * @example 75.05
   */
  avg_asic_temp_c?: number;
  /**
   * The efficiency of the hashboard in joules per terahash.
   * @example 40.05
   */
  efficiency_jth?: number | null;
  /**
   * The current hashrate of the hashboard, measured in GH/s. It will be sum of all ASIC hashrate_ghs values.
   * @example 300.05
   */
  hashrate_ghs?: number;
  /** Manufacturing serial number of the hashboard, used for subsequent API calls. */
  hb_sn?: string;
  /**
   * The expected hashrate is determined by the clock frequency of the all ASIC on the hashboard, measured in GH/s.
   * @example 300.05
   */
  ideal_hashrate_ghs?: number;
  /**
   * The measured temperature at the air intake side of the hashboard.
   * @example 36.19
   */
  inlet_temp_c?: number;
  /**
   * Current maximum temperature of the hashboard in celsius.
   * @example 75.05
   */
  max_asic_temp_c?: number;
  /**
   * The measured temperature at the air exhaust side of the hashboard.
   * @example 78.64
   */
  outlet_temp_c?: number;
  /**
   * The power consumption of the hashboard in watts.
   * @example 1000.05
   */
  power_usage_watts?: number;
  /**
   * The physical slot where the Hashboard is inserted in the system.
   * @example 3
   */
  slot?: number;
  /**
   * The current state or condition of the hashboard.
   * @example "Running"
   */
  status?: "Running" | "Stopped" | "Error" | "Overheated" | "Unknown";
  /**
   * The present voltage being supplied to the hashboard in millivolts.
   * @example 16200.05
   */
  voltage_mv?: number;
}

/** Individual hashboard telemetry metrics. Contains hashboard-specific measurements. The 'asics' field with ASIC-level detail is only populated when the level parameter is 'asic' */
export interface HashboardTelemetry {
  /** ASIC-level telemetry metrics */
  asics?: AsicTelemetry;
  /** A metric value with its unit */
  current?: MetricValue;
  /** A metric value with its unit */
  efficiency: MetricValue;
  /** A metric value with its unit */
  hashrate: MetricValue;
  /** Hashboard index */
  index: number;
  /** A metric value with its unit */
  power: MetricValue;
  /**
   * Hashboard serial number
   * @example "HB001"
   */
  serial_number?: string;
  /** Hashboard temperature measurements */
  temperature: HashboardTemperature;
  /** A metric value with its unit */
  voltage?: MetricValue;
}

/** Hashboard temperature measurements */
export interface HashboardTemperature {
  /** Average temperature value of all temperature sensors on hashboard */
  average: number;
  /** Inlet temperature value */
  inlet: number;
  /** Outlet temperature value */
  outlet: number;
  /** Unit of measurement for metrics */
  unit: MetricUnit;
}

/** Information about all hashboards connected to the mining device */
export interface HashboardsInfo {
  "hashboards-info"?: HashboardInfo[];
}

/** Response containing historical hashrate data over time */
export interface HashrateResponse {
  /** Hashrate data response with time series information */
  "hashrate-data"?: HashrateResponseHashratedata;
}

/** Hashrate data response with time series information */
export interface HashrateResponseHashratedata {
  /** Statistical aggregates for time series data */
  aggregates?: Aggregates;
  data?: TimeSeriesData[];
  /** Duration of time series data returned. */
  duration?: TimeSeriesDuration;
}

/** Hashrate calculated over a specific time window */
export interface HashrateWindow {
  /**
   * Duration of the time window in minutes
   * @example 5
   */
  duration_minutes: number;
  /**
   * Hashrate in TH/s over the time window
   * @example 120.5
   */
  hashrate_ths: number;
}

export interface LocateSystemParams {
  /**
   * The duration in seconds for which to turn on the LED, with a max value of 300 seconds. If not specified, a default value of 30 seconds will be used. Requests made while the LED is on will be ignored.
   * @default 30
   */
  led_on_time?: number;
}

/** System log entries from various sources (OS, miner software, pool software) */
export interface LogsResponse {
  /** Log data response containing system and mining logs */
  logs?: LogsResponseLogs;
}

/** Log data response containing system and mining logs */
export interface LogsResponseLogs {
  content?: string[];
  /**
   * Number of lines returned.
   * @example 100
   */
  lines?: number;
  /**
   * Source of logs.
   * @example "miner_sw"
   */
  source?: string;
}

/** Generic response message */
export interface MessageResponse {
  /** @example "info" */
  message?: string;
}

/** An array of metric values with a shared unit */
export interface MetricArray {
  /** Unit of measurement for metrics */
  unit: MetricUnit;
  /** Array of values where index corresponds to ASIC index */
  values: number[];
}

/**
 * Unit of measurement for metrics
 * @example "TH/s"
 */
export enum MetricUnit {
  THS = "TH/s",
  ValueC = "°C",
  W = "W",
  JTH = "J/TH",
  V = "V",
  A = "A",
}

/** A metric value with its unit */
export interface MetricValue {
  /** Unit of measurement for metrics */
  unit: MetricUnit;
  /** The numeric value of the metric */
  value: number;
}

/** Available field types for miner-level data */
export enum MinerFieldType {
  Hashrate = "hashrate",
  Temperature = "temperature",
  Power = "power",
  Efficiency = "efficiency",
}

/**
 * Miner-level telemetry metrics
 * @example {"hashrate":{"value":95.5,"unit":"TH/s"},"temperature":{"value":65.5,"unit":"°C"},"power":{"value":3250,"unit":"W"},"efficiency":{"value":34.03,"unit":"J/TH"}}
 */
export interface MinerTelemetry {
  /** A metric value with its unit */
  efficiency: MetricValue;
  /** A metric value with its unit */
  hashrate: MetricValue;
  /** A metric value with its unit */
  power: MetricValue;
  /** A metric value with its unit */
  temperature: MetricValue;
}

/** Mining statistics */
export interface MiningStatus {
  /** Mining operation status and performance data */
  "mining-status"?: MiningStatusMiningstatus;
}

/** Mining operation status and performance data */
export interface MiningStatusMiningstatus {
  /**
   * Average temperature of the ASICs in the mining device.
   * @example 60
   */
  average_asic_temp_c?: number;
  /**
   * The average hashrate in giga-hashes per second, since the device started mining. average_hashrate_ghs = Total hash count / (elapsed_time_s * 10^9)
   * @example 110000.2
   */
  average_hashrate_ghs?: number;
  /** The average hashboard efficiency in joules per terahash, since the device started mining. */
  average_hb_efficiency_jth?: number;
  /**
   * Average temperature of the mining device.
   * @example 60
   */
  average_hb_temp_c?: number | null;
  /**
   * The number of hashboards installed (detected) in the miner.
   * @example 4
   */
  hashboards_installed?: number;
  /**
   * The number of hashboards currently in the mining state.
   * @example 4
   */
  hashboards_mining?: number;
  /**
   * The number of hardware errors that have occurred during the mining operation.
   * @example 100
   */
  hw_errors?: number;
  /**
   * Expected hashrate determined by the current power level.
   * @example 112000
   */
  ideal_hashrate_ghs?: number;
  /**
   * The amount of time in seconds that has passed since the start of the mining operation.
   * @example 521
   */
  mining_uptime_s?: number;
  /** The power efficiency in joules per terahash, calculated from total PSU input power and current hashrate. */
  power_efficiency_jth?: number;
  /**
   * Amount of power in watts for the system to target.
   * @example 3120
   */
  power_target_watts?: number;
  /**
   * Amount of power being consumed by mining in watts.
   * @example 3100
   */
  power_usage_watts?: number;
  /**
   * The amount of time in seconds that has passed since the last reboot of the system.
   * @example 521
   */
  reboot_uptime_s?: number;
  /**
   * The indication will reveal whether the mining operation is currently active or has ceased
   * @example "Mining"
   */
  status?:
    | "Uninitialized"
    | "PoweringOn"
    | "Mining"
    | "DegradedMining"
    | "PoweringOff"
    | "Stopped"
    | "NoPools"
    | "Error";
}

/** Mining target configuration for power and performance settings */
export interface MiningTarget {
  /**
   * If enabled, will keep power evenly distributed across bays to balance power phases.
   * @example false
   */
  balance_bays?: boolean;
  /**
   * If true, continue mining even when no valid pools are available. If false, stop mining when no valid pools are available.
   * @example false
   */
  hash_on_disconnect?: boolean;
  /**
   * The performance mode the miner will operate in. Modes:
   *  - MaximumHashrate: Will run at the power target to maximum hashrate.
   *  - Efficiency: Will run at or below the power target to optimize J/TH.
   */
  performance_mode?: PerformanceMode;
  /** @example 3000 */
  power_target_watts?: number;
}

/** Response containing current mining target configuration */
export interface MiningTargetResponse {
  /**
   * Phase balancing configuration state.
   * @example false
   */
  balance_bays?: boolean;
  /** @example 2500 */
  default_power_target_watts?: number;
  /**
   * If true, continue mining even when no valid pools are available. If false, stop mining when no valid pools are available.
   * @example false
   */
  hash_on_disconnect?: boolean;
  /**
   * The performance mode the miner will operate in. Modes:
   *  - MaximumHashrate: Will run at the power target to maximum hashrate.
   *  - Efficiency: Will run at or below the power target to optimize J/TH.
   */
  performance_mode?: PerformanceMode;
  /** @example 3000 */
  power_target_max_watts?: number;
  /** @example 400 */
  power_target_min_watts?: number;
  /** @example 3000 */
  power_target_watts?: number;
}

/**
 * The hashboard performance tuning algorithm
 * @example "None"
 */
export enum MiningTuning {
  None = "None",
  VoltageImbalanceCompensation = "VoltageImbalanceCompensation",
  Fuzzing = "Fuzzing",
}

/** Mining tuning configuration for setting hashboard optimization algorithms */
export interface MiningTuningConfig {
  /** The hashboard performance tuning algorithm */
  algorithm: MiningTuning;
}

/** Network configuration settings for DHCP or static IP setup */
export interface NetworkConfig {
  /** Network configuration settings and parameters */
  "network-config"?: NetworkConfigNetworkconfig;
}

/** Network configuration settings and parameters */
export interface NetworkConfigNetworkconfig {
  /** @example true */
  dhcp?: boolean;
  /** @example "172.27.244.177" */
  gateway?: string;
  /** @example "proto-miner-1" */
  hostname?: string;
  /** @example "172.27.244.179" */
  ip?: string;
  /** @example "255.255.255.240" */
  netmask?: string;
}

/** Network configuration and status information for the mining device */
export interface NetworkInfo {
  /** Network configuration and connection information */
  "network-info"?: NetworkInfoNetworkinfo;
}

/** Network configuration and connection information */
export interface NetworkInfoNetworkinfo {
  /** @example true */
  dhcp?: boolean;
  /** @example "172.27.244.177" */
  gateway?: string;
  /** @example "proto-miner-1" */
  hostname?: string;
  /** @example "172.27.244.179" */
  ip?: string;
  /** @example "82:11:D2:94:0D:6D" */
  mac?: string;
  /** @example "255.255.255.240" */
  netmask?: string;
}

/** Notification error information with source and details */
export interface NotificationError {
  /** @example "FanSlow" */
  error_code?: string;
  /** @example "Fan 3 has stalled. Target RPM: 100, Actual RPM: 0" */
  message?: string;
  slot?: number;
  /** @example "rig" */
  source?: "rig" | "fan" | "psu" | "hashboard";
  /** @example 1764160757 */
  timestamp?: number;
}

/** Operating system information and version details */
export interface OSInfo {
  /** @example "20231208T220633Z" */
  build_datetime_utc?: string;
  /** @example "1213423223" */
  git_hash?: string;
  /** @example "c1-p0" */
  machine?: string;
  /** @example "BTCM Linux Distribution" */
  name?: string;
  /** Operating system status including memory, CPU, and filesystem usage */
  status?: OSStatus;
  /** @example "release" */
  variant?: "release" | "mfg" | "dev" | "unknown";
  /** @example "1.0.1" */
  version?: string;
}

/** Operating system status including memory, CPU, and filesystem usage */
export interface OSStatus {
  /** @example 30.2 */
  cpu_load_percent?: number;
  /** @example 192784 */
  mem_free_kb?: number;
  /** @example 233712 */
  mem_total_kb?: number;
  /** @example 600 */
  rootfs_free_mb?: number;
  /** @example 1024 */
  rootfs_total_mb?: number;
}

/** Pairing information response containing MAC address and serial number */
export interface PairingInfoResponse {
  /**
   * Control board serial number
   * @example "PROTO-B4-001"
   */
  cb_sn: string;
  /**
   * MAC address of the device
   * @example "00:11:22:33:44:55"
   */
  mac: string;
}

/** Password data for authentication operations */
export interface PasswordRequest {
  /**
   * The password for the user
   * @format password
   * @minLength 8
   * @example "securePassword123"
   */
  password: string;
}

/**
 * The performance mode the miner will operate in. Modes:
 *  - MaximumHashrate: Will run at the power target to maximum hashrate.
 *  - Efficiency: Will run at or below the power target to optimize J/TH.
 * @example "MaximumHashrate"
 */
export enum PerformanceMode {
  MaximumHashrate = "MaximumHashrate",
  Efficiency = "Efficiency",
}

/** Mining pool configuration with connection details and priorities */
export interface Pool {
  /**
   * The number of shares that have been accepted by the mining pool as valid solutions to a mining problem.
   * @example 100
   */
  accepted?: number;
  /**
   * The difficulty of best share submitted to the pool.
   * @example 65355
   */
  best_difficulty_share?: number;
  /**
   * The number of mined blocks seen during mining (not necessarily found by miner).
   * @example 10
   */
  blocks_seen?: number;
  /**
   * The current difficulty from the pool.
   * @example 134000
   */
  current_difficulty?: number;
  /**
   * The current number of works in use by the miner.
   * @example 134000
   */
  current_works?: number;
  /**
   * The total difficulty of all accepted shares by the pool.
   * @example 65355
   */
  difficulty_accepted_shares?: number;
  /**
   * The total difficulty of all rejected shares by the pool.
   * @example 134000
   */
  difficulty_rejected_shares?: number;
  /**
   * The number of duplicate shares that were detected and dropped by the pool interface.
   * @example 5
   */
  duplicate?: number;
  /** Hashrate calculated over different time windows. This field provides a flexible array format for future extensions. */
  hashrate?: HashrateWindow[];
  /**
   * Each pool has a unique ID from 0 to 2, with 0 representing the highest priority and 2 representing the lowest priority.
   * @example 0
   */
  id?: number;
  /**
   * The number of shares the pool interface rejected due to being too low difficulty (did not forward to the pool).
   * @example 10
   */
  invalid?: number;
  /**
   * The difficulty of the last share submitted to the pool.
   * @example 134000
   */
  last_share_difficulty?: number;
  /**
   * The time (Unix epoch seconds) of the last share submitted to the pool.
   * @example 65355
   */
  last_share_time?: number;
  /**
   * User-defined display name for this pool. Empty string if not set.
   * @example "Primary Pool"
   */
  name?: string;
  /**
   * The number of notify messages (new jobs) received from the pool.
   * @example 10
   */
  notifys_received?: number;
  /** Connection priority for this pool. Lower numbers are higher priorities, with 0 being the maximum. Duplicate priorities are not allowed. */
  priority?: number;
  /** The protocol being used for communication with the mining pool. */
  protocol?: "Unknown" | "Stratum V1" | "Stratum V2";
  /**
   * The number of shares submitted by the miner to the pool that were not accepted because they did not meet the required difficulty level or other criteria.
   * @example 20
   */
  rejected?: number;
  /** The status field indicates the state of the mining pool. An "Idle" status indicates that the pool is available but not currently in use (due to priority). An "Active" status means that the pool is currently active. A "Dead" status indicates that the mining device is unable to establish a connection with the pool. */
  status?: "Unknown" | "Idle" | "Active" | "Dead";
  /** The pool URL is used to establish communication with the mining pool and it is essential that it includes the port information. */
  url?: PoolUrl;
  /** The user is an account that is used for authentication with the mining pool. In some cases, if the user has multiple mining devices, the pool may assign a worker name as the username for each mining device. */
  user?: PoolUsername;
  /**
   * The number of works that were generated from the job notify messages.
   * @example 10
   */
  works_generated?: number;
}

/** Array of pool configurations for creating or updating pools */
export type PoolConfig = PoolConfigInner[];

/** Individual pool configuration with connection details */
export interface PoolConfigInner {
  /**
   * User-defined display name for this pool.
   * @example "Primary Pool"
   */
  name?: string;
  /** A password used for authentication and accessing the mining pool, which is ignored by SV1 pools. */
  password?: PoolPassword;
  /** The priority of the pool connection. Lower numbers indicate higher priority, with 0 being the highest priority. */
  priority?: PoolPriority;
  /** The pool URL is used to establish communication with the mining pool and it is essential that it includes the port information. */
  url?: PoolUrl;
  /** The user is an account that is used for authentication with the mining pool. In some cases, if the user has multiple mining devices, the pool may assign a worker name as the username for each mining device. */
  username?: PoolUsername;
}

/**
 * A password used for authentication and accessing the mining pool, which is ignored by SV1 pools.
 * @example "anything"
 */
export type PoolPassword = string;

/**
 * The priority of the pool connection. Lower numbers indicate higher priority, with 0 being the highest priority.
 * @example 0
 */
export type PoolPriority = number;

/** Response containing a single pool configuration */
export interface PoolResponse {
  /** Mining pool configuration with connection details and priorities */
  pool?: Pool;
}

/**
 * The pool URL is used to establish communication with the mining pool and it is essential that it includes the port information.
 * @example "stratum+tcp://stratum.braiins.com:3333"
 */
export type PoolUrl = string;

/**
 * The user is an account that is used for authentication with the mining pool. In some cases, if the user has multiple mining devices, the pool may assign a worker name as the username for each mining device.
 * @example "user1"
 */
export type PoolUsername = string;

/** List of configured mining pools with their settings */
export interface PoolsList {
  pools?: Pool[];
}

export interface PostUpdatePsuParams {
  /**
   * Force update and bypass version checks
   * @default false
   */
  force?: boolean;
}

/** Response containing historical power consumption data over time */
export interface PowerResponse {
  /** Power data response with time series information */
  "power-data"?: PowerResponsePowerdata;
}

/** Power data response with time series information */
export interface PowerResponsePowerdata {
  /** Statistical aggregates for time series data */
  aggregates?: Aggregates;
  data?: TimeSeriesData[];
  /** Duration of time series data returned. */
  duration?: TimeSeriesDuration;
}

/** Power supply information including firmware update status */
export interface PowerSuppliesResponse {
  /** PSU firmware update status information */
  psu_update_status?: PsuUpdateStatus;
  psus_info?: PsuInfo[];
}

/** Available field types for PSU-level data */
export enum PsuFieldType {
  OutputVoltage = "outputVoltage",
  OutputCurrent = "outputCurrent",
  OutputPower = "outputPower",
  InputVoltage = "inputVoltage",
  InputCurrent = "inputCurrent",
  InputPower = "inputPower",
  HotspotTemp = "hotspotTemp",
  AmbientTemp = "ambientTemp",
  AverageTemp = "averageTemp",
}

/** Per-PSU firmware update progress */
export interface PsuFwupProgress {
  /** Optional status message (firmware name on start, error on failure) */
  message?: string;
  /**
   * Upload progress percentage (0-100)
   * @format float
   */
  progress_percent: number;
  /** The physical slot where the PSU is inserted in the system. (1-3) */
  psu_slot: number;
  /** Current firmware update state */
  state: "idle" | "uploading" | "verifying" | "complete" | "failed";
}

/** Power supply unit information and status */
export interface PsuInfo {
  firmware?: {
    /**
     * Firmware application version.
     * @example "1.0"
     */
    app_version?: string;
    /**
     * Firmware bootloader version.
     * @example "1.0"
     */
    bootloader_version?: string;
  };
  /**
   * Hardware revision.
   * @example "v1.0"
   */
  hw_revision?: string;
  /**
   * PSU manufacturer.
   * @example "Chicony"
   */
  manufacturer?: string;
  /**
   * Model name or number.
   * @example ""
   */
  model?: string;
  power?: {
    /**
     * Input current in milliamperes.
     * @example 20340
     */
    input_current_ma?: number;
    /**
     * Input power in milliwatts.
     * @example 386800
     */
    input_power_mw?: number;
    /**
     * Input voltage in millivolts.
     * @example 24000
     */
    input_voltage_mv?: number;
    /**
     * Output current in milliamperes.
     * @example 25150
     */
    output_current_ma?: number;
    /**
     * Output power in milliwatts.
     * @example 400000
     */
    output_power_mw?: number;
    /**
     * Output voltage in millivolts.
     * @example 15380
     */
    output_voltage_mv?: number;
  };
  /**
   * Power supply serial number.
   * @example "517CP81302000721"
   */
  psu_sn?: string;
  /**
   * The physical slot where the PSU is inserted in the system. (1-3)
   * @example 2
   */
  slot?: number;
  temperatures?: TemperatureMeasurement[];
  /**
   * Vendor name.
   * @example ""
   */
  vendor?: string;
}

/** PSU metric with input and output values */
export interface PsuInputOutputMetric {
  /** Input value */
  input: number;
  /** Output value */
  output: number;
  /** Unit of measurement for metrics */
  unit: MetricUnit;
}

/** Individual PSU telemetry metrics */
export interface PsuTelemetry {
  /** PSU metric with input and output values */
  current: PsuInputOutputMetric;
  /** PSU index */
  index: number;
  /** PSU metric with input and output values */
  power: PsuInputOutputMetric;
  /**
   * PSU serial number
   * @example "PSU001234"
   */
  serial_number?: string;
  /** PSU temperature measurements */
  temperature: PsuTemperature;
  /** PSU metric with input and output values */
  voltage: PsuInputOutputMetric;
}

/** PSU temperature measurements */
export interface PsuTemperature {
  /** Ambient temperature value */
  ambient: number;
  /** Average temperature value of all temperature sensors on PSU */
  average: number;
  /** Hotspot temperature value */
  hotspot: number;
  /** Unit of measurement for metrics */
  unit: MetricUnit;
}

/**
 * PSU firmware update status information
 * @example {"available_firmware":[{"filename":"chicony-s24-1.18.22.bin","firmware_version":"1.18.22","size_bytes":1048576,"sha256":"b5f8a2f4e5c19ac7e2e4d0a87f3a1a6b6e0c8b991f2e99a4f5cd5e512a5f4d1e3","model":"S24"}],"psu_fw_status":[{"psu_slot":1,"state":"uploading","progress_percent":45.2,"message":"chicony-s24-1.18.22.bin"}]}
 */
export interface PsuUpdateStatus {
  available_firmware?: AvailablePsuFirmware[];
  /** Per-PSU firmware update progress (only present for PSUs that have received progress messages) */
  psu_fw_status?: PsuFwupProgress[];
}

/** Information about all power supply units in the mining device */
export interface PsusInfo {
  "psus-info"?: PsuInfo[];
}

/** Request data for refreshing JWT access tokens */
export interface RefreshRequest {
  /**
   * The JWT refresh token to be validated.
   * @example "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
   */
  refresh_token: string;
}

/** Response containing a new JWT access token */
export interface RefreshResponse {
  /**
   * A new JWT access token.
   * @example "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
   */
  access_token: string;
}

/** Software component name and version information */
export interface SWInfo {
  /** @example "Cgminer" */
  name?: string;
  /** @example "1.0" */
  version?: string;
}

/** Response indicating whether the system is in a secure state */
export interface SecureResponse {
  /**
   * True if the device is locked and the device certificate is valid
   * @example true
   */
  secure: boolean;
}

/** Request to set the authentication public key */
export interface SetAuthKeyRequest {
  /**
   * EdDSA public key as base64-encoded DER (SPKI)
   * @example "MCowBQYDK2VwAyEAGb1gauf6Rn+VgMwMeMHFBjBLHaiv83R1RV5MhFqxTW0="
   */
  public_key: string;
}

/** Response after setting or clearing the auth key */
export interface SetAuthKeyResponse {
  /**
   * Status message
   * @example "Auth key set successfully"
   */
  message: string;
}

/** Configuration for SSH access */
export interface SshConfig {
  /** SSH service status information */
  "ssh-status"?: SshStatus;
}

/** Response containing SSH status */
export interface SshResponse {
  /** SSH service status information */
  "ssh-status"?: SshStatus;
}

/** SSH service status information */
export interface SshStatus {
  /** @example true */
  enabled?: boolean;
}

/** Complete system information including hardware, software, and OS details */
export interface SystemInfo {
  /** System information and device details */
  "system-info"?: SystemInfoSysteminfo;
}

/** System information and device details */
export interface SystemInfoSysteminfo {
  /** @example "C3" */
  board?: "C1" | "C2" | "C3" | "Unknown";
  /** @example "YWWLMMMMRRFSSSSS" */
  cb_sn?: string;
  /** Software component name and version information */
  hashboard_firmware?: SWInfo;
  /**
   * Device manufacturer name.
   * @example "Proto"
   */
  manufacturer?: string;
  /** Software component name and version information */
  mining_driver_sw?: SWInfo;
  /**
   * Device model identifier (without manufacturer prefix).
   * @example "Rig"
   */
  model?: string;
  /** Operating system information and version details */
  os?: OSInfo;
  /** Software component name and version information */
  pool_interface_sw?: SWInfo;
  /**
   * Product name reported by the device.
   * @example "Proto Rig"
   */
  product_name?: string;
  /** @example "STM32MP157F" */
  soc?: "STM32MP157F" | "STM32MP157D" | "STM32MP151F" | "STM32MP131F" | "unknown";
  /** Current status and information about system software updates */
  sw_update_status?: UpdateStatus;
  /**
   * @format int64
   * @example 300
   */
  uptime_seconds?: number;
  /** Software component name and version information */
  web_dashboard?: SWInfo;
  /** Software component name and version information */
  web_server?: SWInfo;
}

/** System status information including onboarding and password setup */
export interface SystemStatuses {
  /**
   * True when the device is still using the factory default password. Most API endpoints return 403 until the password is changed.
   * @example false
   */
  default_password_active?: boolean;
  /** @example true */
  onboarded?: boolean;
  /** @example true */
  password_set?: boolean;
}

/** Configuration for telemetry data collection */
export interface TelemetryConfig {
  /**
   * Enable or disable telemetry
   * @example true
   */
  enabled: boolean;
}

/** Current telemetry data response. Contains 'miner' field with aggregated metrics (included when level=miner or by default if no level specified), 'hashboards' array with per-hashboard data (included when level=hashboard or level=asic), 'psus' array with per-PSU data (included when level=psu). ASIC data is nested within each hashboard when level=asic is specified. All fields except 'timestamp' are optional based on requested levels. */
export interface TelemetryData {
  /**
   * Array of per-hashboard telemetry data. Included when level=hashboard or level=asic is specified. Each hashboard object contains its metrics, with ASIC-level data nested within when level=asic is specified.
   * @example [{"index":0,"serial_number":"HB001","hashrate":{"value":31.8,"unit":"TH/s"},"temperature":{"unit":"°C","inlet":45.2,"outlet":68.5,"average":56.85},"power":{"value":1080,"unit":"W"},"efficiency":{"value":33.96,"unit":"J/TH"},"voltage":{"value":12.1,"unit":"V"},"current":{"value":89.3,"unit":"A"},"asics":{"hashrate":{"unit":"TH/s","values":[0.265,0.264]},"temperature":{"unit":"°C","values":[72.5,73]}}}]
   */
  hashboards?: HashboardTelemetry[];
  /** Miner-level telemetry metrics */
  miner?: MinerTelemetry;
  /**
   * Array of per-PSU telemetry data. Included when level=psu is specified.
   * @example [{"index":0,"serial_number":"PSU001234","voltage":{"unit":"V","input":240,"output":12.1},"current":{"unit":"A","input":14.5,"output":268.6},"power":{"unit":"W","input":3480,"output":3250},"temperature":{"unit":"°C","hotspot":65.5,"ambient":45.2,"average":55.4}}]
   */
  psus?: PsuTelemetry[];
  /**
   * Timestamp when the telemetry data was collected
   * @format date-time
   * @example "2024-01-15T14:30:00Z"
   */
  timestamp: string;
}

/** Response containing telemetry status information */
export interface TelemetryResponse {
  /**
   * Whether telemetry is currently enabled
   * @example true
   */
  enabled: boolean;
  /**
   * Status message about telemetry
   * @example "Telemetry is enabled"
   */
  message: string;
}

/** Temperature measurement from a sensor */
export interface TemperatureMeasurement {
  /**
   * Temperature in Celsius.
   * @example 40
   */
  temperature_c?: number;
  /**
   * Type of temperature measurement.
   * @example "hotspot"
   */
  temperature_type?: string;
}

/** Response containing historical temperature data over time */
export interface TemperatureResponse {
  /** Temperature data response with time series information */
  "temperature-data"?: TemperatureResponseTemperaturedata;
}

/** Temperature data response with time series information */
export interface TemperatureResponseTemperaturedata {
  /** Statistical aggregates for time series data */
  aggregates?: Aggregates;
  data?: TimeSeriesData[];
  /** Duration of time series data returned. */
  duration?: TimeSeriesDuration;
}

/** Configuration for testing connection to a mining pool */
export interface TestConnection {
  /** A password used for authentication and accessing the mining pool, which is ignored by SV1 pools. */
  password?: PoolPassword;
  /** The pool URL is used to establish communication with the mining pool and it is essential that it includes the port information. */
  url?: PoolUrl;
  /** The user is an account that is used for authentication with the mining pool. In some cases, if the user has multiple mining devices, the pool may assign a worker name as the username for each mining device. */
  username?: PoolUsername;
}

/** Statistical aggregates for the entire time series */
export interface TimeSeriesAggregates {
  /**
   * Average value in the series
   * @example 94.75
   */
  avg?: number;
  /**
   * Maximum value in the series
   * @example 95.5
   */
  max?: number;
  /**
   * Minimum value in the series
   * @example 94
   */
  min?: number;
}

/** Time series data point with timestamp and value for historical metrics */
export interface TimeSeriesData {
  /**
   * Unix time epoch.
   * @example 1704067200
   */
  datetime?: number;
  /**
   * Value of data requested at the given datetime.
   * @example 95.5
   */
  value?: number | null;
}

/**
 * Duration of time series data returned.
 * @example "24h"
 */
export enum TimeSeriesDuration {
  Value1H = "1h",
  Value12H = "12h",
  Value24H = "24h",
  Value48H = "48h",
  Value5D = "5d",
}

/** Configuration for a specific level in time series query */
export type TimeSeriesLevelConfig =
  | {
      /**
       * List of data types to retrieve for miner level
       * @minItems 1
       * @example ["hashrate","temperature","power"]
       */
      fields: MinerFieldType[];
      /** Miner level type */
      type: "miner";
    }
  | {
      /**
       * List of data types to retrieve for hashboard level
       * @minItems 1
       * @example ["hashrate","inletTemp","outletTemp"]
       */
      fields: HashboardFieldType[];
      /**
       * Optional array of zero-based indexes to filter data. If omitted, returns all available items
       * @example [0,2]
       */
      indexes?: number[];
      /** Hashboard level type */
      type: "hashboard";
    }
  | {
      /**
       * List of data types to retrieve for ASIC level
       * @minItems 1
       * @example ["hashrate","temperature"]
       */
      fields: AsicFieldType[];
      /**
       * Optional array of zero-based ASIC indexes to filter data. If omitted, returns all available ASICs
       * @example [0,2]
       */
      indexes?: number[];
      /** ASIC level type */
      type: "asic";
    }
  | {
      /**
       * List of data types to retrieve for PSU level
       * @minItems 1
       * @example ["inputVoltage","outputPower","hotspotTemp"]
       */
      fields: PsuFieldType[];
      /**
       * Optional array of zero-based PSU indexes to filter data. If omitted, returns all available PSUs
       * @example [0,1]
       */
      indexes?: number[];
      /** PSU level type */
      type: "psu";
    };

/** Metadata about the time series query and response */
export interface TimeSeriesMeta {
  /**
   * Aggregation method used
   * @example "mean"
   */
  aggregation?: string;
  /**
   * Actual end time of the data
   * @format date-time
   * @example "2024-01-15T06:00:00Z"
   */
  end_time?: string;
  /**
   * ISO 8601 duration of the interval used
   * @example "PT15M"
   */
  interval?: string;
  /**
   * Array of level configurations that were requested
   * @example [{"type":"miner","fields":["hashrate","temperature"]},{"type":"hashboard","fields":["hashrate","temperature"]}]
   */
  levels?: TimeSeriesLevelConfig[];
  /**
   * Actual start time of the data
   * @format date-time
   * @example "2024-01-15T00:00:00Z"
   */
  start_time?: string;
}

/** Data series for a specific metric */
export interface TimeSeriesMetricData {
  /** Statistical aggregates for the entire time series */
  aggregates?: TimeSeriesAggregates;
  /** Unit of measurement for metrics */
  unit?: MetricUnit;
  /**
   * Array of values at each time interval
   * @example [95,94.5,94.8]
   */
  values?: (number | null)[];
}

/** Request parameters for time series data query */
export interface TimeSeriesRequest {
  /**
   * Aggregation method for data within intervals
   * @default "mean"
   * @example "mean"
   */
  aggregation?: "mean" | "avg" | "min" | "max" | "last" | "sum" | "count";
  /**
   * ISO 8601 duration as an alternative to end_time. Mutually exclusive with end_time.
   * @pattern ^P(\d+Y)?(\d+M)?(\d+D)?(T(\d+H)?(\d+M)?(\d+S)?)?$
   * @example "PT6H"
   */
  duration?: string;
  /**
   * End of the time range in ISO 8601 format. If not specified, defaults to current time.
   * @format date-time
   * @example "2024-01-15T12:00:00Z"
   */
  end_time?: string;
  /**
   * ISO 8601 duration for the time interval between data points. If not specified, auto-calculated based on time range.
   * @pattern ^P(\d+Y)?(\d+M)?(\d+D)?(T(\d+H)?(\d+M)?(\d+S)?)?$
   * @example "PT5M"
   */
  interval?: string;
  /**
   * Array of level configurations. Each object specifies a level type, fields to retrieve, and optional indexes.
   * @minItems 1
   */
  levels: TimeSeriesLevelConfig[];
  /**
   * Start of the time range in ISO 8601 format
   * @format date-time
   * @example "2024-01-15T00:00:00Z"
   */
  start_time: string;
}

/** Response containing time series data for requested metrics */
export interface TimeSeriesResponse {
  /** Hierarchical data organized by level */
  data?: {
    /** Array of ASIC-level data with zero-based indexing (present when 'asic' in levels) */
    asics?: {
      /** Zero-based index of the hashboard this ASIC belongs to */
      hashboard_index: number;
      /** Zero-based index of the ASIC within its hashboard */
      index: number;
      [key: string]: any;
    }[];
    /** Array of hashboard-level data with zero-based indexing (present when 'hashboard' in levels) */
    hashboards?: {
      /** Zero-based index of the hashboard */
      index?: number;
      /** Serial number of the hashboard */
      serial_number?: string;
      [key: string]: any;
    }[];
    /** Miner-level data (present when 'miner' in levels) */
    miner?: Record<string, TimeSeriesMetricData>;
    /** Array of PSU-level data with zero-based indexing (present when 'psu' in levels) */
    psus?: {
      /** Zero-based index of the PSU */
      index?: number;
      /** Serial number of the PSU */
      serial_number?: string;
      [key: string]: any;
    }[];
  };
  /** Metadata about the time series query and response */
  meta?: TimeSeriesMeta;
}

/** Configuration for device unlock operation */
export interface UnlockConfig {
  /** @example "unlock123" */
  "unlock-password"?: string;
}

/** Response containing device lock status */
export interface UnlockResponse {
  /** @example "UNLOCKED" */
  "lock-status"?: string;
}

/** Current status and information about system software updates */
export interface UpdateStatus {
  /**
   * Current software version
   * @example "1.0.0"
   */
  current_version?: string;
  /**
   * Error message if status is 'error'
   * @example "Download failed"
   */
  error?: string;
  /**
   * Human-readable message about the update status
   * @example "Update available"
   */
  message?: string;
  /**
   * Version of the available update
   * @example "1.1.0"
   */
  new_version?: string;
  /**
   * Previous software version
   * @example "1.0.0"
   */
  previous_version?: string;
  /**
   * Progress percentage for downloading or installing (0-100)
   * @example 75
   */
  progress?: number;
  /**
   * Release notes for the available update
   * @example "Bug fixes and performance improvements"
   */
  release_notes?: string;
  /**
   * Current status of the software update process
   * @example "available"
   */
  status?:
    | "current"
    | "available"
    | "downloading"
    | "downloaded"
    | "installing"
    | "installed"
    | "confirming"
    | "success"
    | "error";
}

export type QueryParamsType = Record<string | number, any>;
export type ResponseFormat = keyof Omit<Body, "body" | "bodyUsed">;

export interface FullRequestParams extends Omit<RequestInit, "body"> {
  /** set parameter to `true` for call `securityWorker` for this request */
  secure?: boolean;
  /** request path */
  path: string;
  /** content type of request body */
  type?: ContentType;
  /** query params */
  query?: QueryParamsType;
  /** format of response (i.e. response.json() -> format: "json") */
  format?: ResponseFormat;
  /** request body */
  body?: unknown;
  /** base url */
  baseUrl?: string;
  /** request cancellation token */
  cancelToken?: CancelToken;
}

export type RequestParams = Omit<FullRequestParams, "body" | "method" | "query" | "path">;

export interface ApiConfig<SecurityDataType = unknown> {
  baseUrl?: string;
  baseApiParams?: Omit<RequestParams, "baseUrl" | "cancelToken" | "signal">;
  securityWorker?: (securityData: SecurityDataType | null) => Promise<RequestParams | void> | RequestParams | void;
  customFetch?: typeof fetch;
}

export interface HttpResponse<D extends unknown, E extends unknown = unknown> extends Response {
  data: D;
  error: E;
}

type CancelToken = Symbol | string | number;

export enum ContentType {
  Json = "application/json",
  JsonApi = "application/vnd.api+json",
  FormData = "multipart/form-data",
  UrlEncoded = "application/x-www-form-urlencoded",
  Text = "text/plain",
}

export class HttpClient<SecurityDataType = unknown> {
  public baseUrl: string = "";
  private securityData: SecurityDataType | null = null;
  private securityWorker?: ApiConfig<SecurityDataType>["securityWorker"];
  private abortControllers = new Map<CancelToken, AbortController>();
  private customFetch = (...fetchParams: Parameters<typeof fetch>) => fetch(...fetchParams);

  private baseApiParams: RequestParams = {
    credentials: "same-origin",
    headers: {},
    redirect: "follow",
    referrerPolicy: "no-referrer",
  };

  constructor(apiConfig: ApiConfig<SecurityDataType> = {}) {
    Object.assign(this, apiConfig);
  }

  public setSecurityData = (data: SecurityDataType | null) => {
    this.securityData = data;
  };

  protected encodeQueryParam(key: string, value: any) {
    const encodedKey = encodeURIComponent(key);
    return `${encodedKey}=${encodeURIComponent(typeof value === "number" ? value : `${value}`)}`;
  }

  protected addQueryParam(query: QueryParamsType, key: string) {
    return this.encodeQueryParam(key, query[key]);
  }

  protected addArrayQueryParam(query: QueryParamsType, key: string) {
    const value = query[key];
    return value.map((v: any) => this.encodeQueryParam(key, v)).join("&");
  }

  protected toQueryString(rawQuery?: QueryParamsType): string {
    const query = rawQuery || {};
    const keys = Object.keys(query).filter((key) => "undefined" !== typeof query[key]);
    return keys
      .map((key) => (Array.isArray(query[key]) ? this.addArrayQueryParam(query, key) : this.addQueryParam(query, key)))
      .join("&");
  }

  protected addQueryParams(rawQuery?: QueryParamsType): string {
    const queryString = this.toQueryString(rawQuery);
    return queryString ? `?${queryString}` : "";
  }

  private contentFormatters: Record<ContentType, (input: any) => any> = {
    [ContentType.Json]: (input: any) =>
      input !== null && (typeof input === "object" || typeof input === "string") ? JSON.stringify(input) : input,
    [ContentType.JsonApi]: (input: any) =>
      input !== null && (typeof input === "object" || typeof input === "string") ? JSON.stringify(input) : input,
    [ContentType.Text]: (input: any) => (input !== null && typeof input !== "string" ? JSON.stringify(input) : input),
    [ContentType.FormData]: (input: any) => {
      if (input instanceof FormData) {
        return input;
      }

      return Object.keys(input || {}).reduce((formData, key) => {
        const property = input[key];
        formData.append(
          key,
          property instanceof Blob
            ? property
            : typeof property === "object" && property !== null
              ? JSON.stringify(property)
              : `${property}`,
        );
        return formData;
      }, new FormData());
    },
    [ContentType.UrlEncoded]: (input: any) => this.toQueryString(input),
  };

  protected mergeRequestParams(params1: RequestParams, params2?: RequestParams): RequestParams {
    return {
      ...this.baseApiParams,
      ...params1,
      ...(params2 || {}),
      headers: {
        ...(this.baseApiParams.headers || {}),
        ...(params1.headers || {}),
        ...((params2 && params2.headers) || {}),
      },
    };
  }

  protected createAbortSignal = (cancelToken: CancelToken): AbortSignal | undefined => {
    if (this.abortControllers.has(cancelToken)) {
      const abortController = this.abortControllers.get(cancelToken);
      if (abortController) {
        return abortController.signal;
      }
      return void 0;
    }

    const abortController = new AbortController();
    this.abortControllers.set(cancelToken, abortController);
    return abortController.signal;
  };

  public abortRequest = (cancelToken: CancelToken) => {
    const abortController = this.abortControllers.get(cancelToken);

    if (abortController) {
      abortController.abort();
      this.abortControllers.delete(cancelToken);
    }
  };

  public request = async <T = any, E = any>({
    body,
    secure,
    path,
    type,
    query,
    format,
    baseUrl,
    cancelToken,
    ...params
  }: FullRequestParams): Promise<HttpResponse<T, E>> => {
    const secureParams =
      ((typeof secure === "boolean" ? secure : this.baseApiParams.secure) &&
        this.securityWorker &&
        (await this.securityWorker(this.securityData))) ||
      {};
    const requestParams = this.mergeRequestParams(params, secureParams);
    const queryString = query && this.toQueryString(query);
    const payloadFormatter = this.contentFormatters[type || ContentType.Json];
    const responseFormat = format || requestParams.format;

    return this.customFetch(`${baseUrl || this.baseUrl || ""}${path}${queryString ? `?${queryString}` : ""}`, {
      ...requestParams,
      headers: {
        ...(requestParams.headers || {}),
        ...(type && type !== ContentType.FormData ? { "Content-Type": type } : {}),
      },
      signal: (cancelToken ? this.createAbortSignal(cancelToken) : requestParams.signal) || null,
      body: typeof body === "undefined" || body === null ? null : payloadFormatter(body),
    }).then(async (response) => {
      const r = response as HttpResponse<T, E>;
      r.data = null as unknown as T;
      r.error = null as unknown as E;

      const responseToParse = responseFormat ? response.clone() : response;
      const data = !responseFormat
        ? r
        : await responseToParse[responseFormat]()
            .then((data) => {
              if (r.ok) {
                r.data = data;
              } else {
                r.error = data;
              }
              return r;
            })
            .catch((e) => {
              r.error = e;
              return r;
            });

      if (cancelToken) {
        this.abortControllers.delete(cancelToken);
      }

      if (!response.ok) throw data;
      return data;
    });
  };
}

/**
 * @title Mining Development Kit API
 * @version 1.8.0
 * @license MIT (https://opensource.org/license/mit)
 * @baseUrl http://127.0.0.1:8080
 * @contact <mining.support@block.xyz>
 *
 * The Mining Development Kit API serves as a means to access information from the mining device and make necessary adjustments to its settings.
 */
export class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
  api = {
    /**
     * @description The get pools endpoint returns the full list of currently configured pools.
     *
     * @tags Pools
     * @name ListPools
     * @request GET:/api/v1/pools
     */
    listPools: (params: RequestParams = {}) =>
      this.request<PoolsList, MessageResponse>({
        path: `/api/v1/pools`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The post pools endpoint allows up to three pools to be configured, replacing the previous pool configuration.
     *
     * @tags Pools
     * @name CreatePools
     * @request POST:/api/v1/pools
     * @secure
     */
    createPools: (data: PoolConfig, params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/pools`,
        method: "POST",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description Get configuration for a specific pool by ID
     *
     * @tags Pools
     * @name GetPool
     * @request GET:/api/v1/pools/{id}
     */
    getPool: ({ id }: GetPoolParams, params: RequestParams = {}) =>
      this.request<PoolResponse, MessageResponse>({
        path: `/api/v1/pools/${id}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description Using this pool configuration endpoint, users can edit the properties of an existing pool.
     *
     * @tags Pools
     * @name EditPool
     * @request PUT:/api/v1/pools/{id}
     * @secure
     */
    editPool: ({ id }: EditPoolParams, data: PoolConfigInner, params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/pools/${id}`,
        method: "PUT",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description Delete a specific pool configuration by ID
     *
     * @tags Pools
     * @name DeletePool
     * @request DELETE:/api/v1/pools/{id}
     * @secure
     */
    deletePool: ({ id }: DeletePoolParams, params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/pools/${id}`,
        method: "DELETE",
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * @description Used to test a pool connection
     *
     * @tags Pools
     * @name TestPoolConnection
     * @request POST:/api/v1/pools/test-connection
     */
    testPoolConnection: (data: TestConnection, params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/pools/test-connection`,
        method: "POST",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description The password endpoint allows users to set a password during onboarding
     *
     * @tags Authentication
     * @name SetPassword
     * @request PUT:/api/v1/auth/password
     */
    setPassword: (data: PasswordRequest, params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/auth/password`,
        method: "PUT",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description Change the current password to a new password. Requires the current password for verification.
     *
     * @tags Authentication
     * @name ChangePassword
     * @request PUT:/api/v1/auth/change-password
     * @secure
     */
    changePassword: (data: ChangePasswordRequest, params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/auth/change-password`,
        method: "PUT",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description Authenticates a user using a password and returns a JWT access and refresh token pair.
     *
     * @tags Authentication
     * @name Login
     * @request POST:/api/v1/auth/login
     */
    login: (data: PasswordRequest, params: RequestParams = {}) =>
      this.request<AuthTokens, MessageResponse>({
        path: `/api/v1/auth/login`,
        method: "POST",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description Validates and blacklists JWT tokens, effectively logging out the user.
     *
     * @tags Authentication
     * @name Logout
     * @summary User logout
     * @request POST:/api/v1/auth/logout
     * @secure
     */
    logout: (data: AuthTokens, params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/auth/logout`,
        method: "POST",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description Validates the provided refresh token and returns a new JWT access token.
     *
     * @tags Authentication
     * @name RefreshToken
     * @summary Refresh JWT access token
     * @request POST:/api/v1/auth/refresh
     */
    refreshToken: (data: RefreshRequest, params: RequestParams = {}) =>
      this.request<RefreshResponse, MessageResponse>({
        path: `/api/v1/auth/refresh`,
        method: "POST",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description The system endpoint provides information related to the control board including OS, software, and hardware component details.
     *
     * @tags System
     * @name GetSystemInfo
     * @request GET:/api/v1/system
     */
    getSystemInfo: (params: RequestParams = {}) =>
      this.request<SystemInfo, any>({
        path: `/api/v1/system`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description Get system statuses
     *
     * @tags System Information
     * @name GetSystemStatus
     * @request GET:/api/v1/system/status
     */
    getSystemStatus: (params: RequestParams = {}) =>
      this.request<SystemStatuses, any>({
        path: `/api/v1/system/status`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The mining endpoint provides summary information about the mining operations of the device. This includes device level hashrate statistics, overall miner status, and current power usage and target information.
     *
     * @tags Mining
     * @name GetMiningStatus
     * @request GET:/api/v1/mining
     */
    getMiningStatus: (params: RequestParams = {}) =>
      this.request<MiningStatus, MessageResponse>({
        path: `/api/v1/mining`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The mining target endpoint returns the current power target in watts that the miner is controlling for.
     *
     * @tags Mining
     * @name GetMiningTarget
     * @request GET:/api/v1/mining/target
     */
    getMiningTarget: (params: RequestParams = {}) =>
      this.request<MiningTargetResponse, MessageResponse>({
        path: `/api/v1/mining/target`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The mining target endpoint can be used to set a target power consumption for the miner. Once set, the mining device will operate to consume as close to that amount of power as possible. In the event that the device is unable to maintain its temperature within the allowed range, it may scale down and use less power.
     *
     * @tags Mining
     * @name EditMiningTarget
     * @request PUT:/api/v1/mining/target
     * @secure
     */
    editMiningTarget: (data: MiningTarget, params: RequestParams = {}) =>
      this.request<MiningTargetResponse, MessageResponse>({
        path: `/api/v1/mining/target`,
        method: "PUT",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description The mining tuning endpoint can be used to set a hashboard level optimization algorithm
     *
     * @tags Mining
     * @name EditMiningTuning
     * @request PUT:/api/v1/mining/tuning
     * @secure
     */
    editMiningTuning: (data: MiningTuningConfig, params: RequestParams = {}) =>
      this.request<MiningTuningConfig, MessageResponse>({
        path: `/api/v1/mining/tuning`,
        method: "PUT",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description The start mining endpoint can be used to make the device start mining, into account the current power target of the system.
     *
     * @tags Mining
     * @name StartMining
     * @request POST:/api/v1/mining/start
     * @secure
     */
    startMining: (params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/mining/start`,
        method: "POST",
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * @description The stop mining endpoint can be used to stop the device from mining, going into a minimal power mode with only the control board running.
     *
     * @tags Mining
     * @name StopMining
     * @request POST:/api/v1/mining/stop
     * @secure
     */
    stopMining: (params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/mining/stop`,
        method: "POST",
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * @description The reboot endpoint can be used to reboot the entire system.
     *
     * @tags System
     * @name RebootSystem
     * @request POST:/api/v1/system/reboot
     * @secure
     */
    rebootSystem: (params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/system/reboot`,
        method: "POST",
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * @description The locate system endpoint can be used to flash the indicator LED on the control board to assist in finding the miner.
     *
     * @tags System
     * @name LocateSystem
     * @request POST:/api/v1/system/locate
     * @secure
     */
    locateSystem: (query: LocateSystemParams, params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/system/locate`,
        method: "POST",
        query: query,
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * @description The logs endpoint provides the most recent log lines from a given source, either OS, pool software, or miner logs.
     *
     * @tags System
     * @name GetSystemLogs
     * @request GET:/api/v1/system/logs
     */
    getSystemLogs: (query: GetSystemLogsParams, params: RequestParams = {}) =>
      this.request<LogsResponse, MessageResponse>({
        path: `/api/v1/system/logs`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description Initiates a check with the update server to determine whether a new version of the miner software is available. This request does not perform a download or installation, only a version availability check.
     *
     * @tags System
     * @name UpdateCheck
     * @request POST:/api/v1/system/update/check
     */
    updateCheck: (params: RequestParams = {}) =>
      this.request<void, MessageResponse>({
        path: `/api/v1/system/update/check`,
        method: "POST",
        ...params,
      }),

    /**
     * @description Initiates a system update of the miner software. This will download the update and automatically install it once the download completes.
     *
     * @tags System
     * @name PostUpdateSystem
     * @request POST:/api/v1/system/update
     * @secure
     */
    postUpdateSystem: (params: RequestParams = {}) =>
      this.request<void, MessageResponse>({
        path: `/api/v1/system/update`,
        method: "POST",
        secure: true,
        ...params,
      }),

    /**
     * @description Uploads a firmware update file to the device. This endpoint will also install it once the upload completes.
     *
     * @tags System
     * @name PutUpdateSystem
     * @request PUT:/api/v1/system/update
     * @secure
     */
    putUpdateSystem: (
      data: {
        /**
         * The firmware update file to upload (.swu format)
         * @format binary
         * @example "firmware-update-v2.0.2.swu"
         */
        file: File;
      },
      params: RequestParams = {},
    ) =>
      this.request<void, MessageResponse>({
        path: `/api/v1/system/update`,
        method: "PUT",
        body: data,
        secure: true,
        type: ContentType.FormData,
        ...params,
      }),

    /**
     * @description The get ssh endpoint returns if SSH is enabled or disabled on the control board
     *
     * @tags System
     * @name GetSsh
     * @request GET:/api/v1/system/ssh
     */
    getSsh: (params: RequestParams = {}) =>
      this.request<SshResponse, MessageResponse>({
        path: `/api/v1/system/ssh`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The put ssh endpoint enables/disables SSH on the control board
     *
     * @tags System
     * @name SetSsh
     * @request PUT:/api/v1/system/ssh
     * @secure
     */
    setSsh: (data: SshConfig, params: RequestParams = {}) =>
      this.request<SshResponse, MessageResponse>({
        path: `/api/v1/system/ssh`,
        method: "PUT",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description Returns whether the system is in a secure state. A system is secure when the device is locked (CLOSED) and the device certificate is valid.
     *
     * @tags System
     * @name GetSecureStatus
     * @request GET:/api/v1/system/secure
     */
    getSecureStatus: (params: RequestParams = {}) =>
      this.request<SecureResponse, MessageResponse>({
        path: `/api/v1/system/secure`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The get UNLOCK endpoint returns current lock status of the control board.
     *
     * @tags System
     * @name GetUnlock
     * @request GET:/api/v1/system/unlock
     */
    getUnlock: (params: RequestParams = {}) =>
      this.request<UnlockResponse, MessageResponse>({
        path: `/api/v1/system/unlock`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The put UNLOCK endpoint execute device unlock on the control board when correct password is used.
     *
     * @tags System
     * @name SetUnlock
     * @request PUT:/api/v1/system/unlock
     * @secure
     */
    setUnlock: (data: UnlockConfig, params: RequestParams = {}) =>
      this.request<UnlockResponse, MessageResponse>({
        path: `/api/v1/system/unlock`,
        method: "PUT",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description The hashboards endpoint provides information about all of the hashboards connected to the system, including firmware version, MCU, ASIC count, API version, and hardware serial numbers.
     *
     * @tags Hashboards
     * @name GetAllHashboards
     * @request GET:/api/v1/hashboards
     */
    getAllHashboards: (params: RequestParams = {}) =>
      this.request<HashboardsInfo, MessageResponse>({
        path: `/api/v1/hashboards`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The hashboard status endpoint returns current operating statistics for a single hashboard in the system based on its serial number.
     *
     * @tags Hashboards
     * @name GetHashboardStatus
     * @request GET:/api/v1/hashboards/{hb_sn}
     */
    getHashboardStatus: ({ hbSn }: GetHashboardStatusParams, params: RequestParams = {}) =>
      this.request<HashboardStats, MessageResponse>({
        path: `/api/v1/hashboards/${hbSn}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The hashboard status endpoint returns current operating statistics for a single ASIC on the specified hashboard in the system based on serial number and ASIC ID.
     *
     * @tags Hashboards
     * @name GetAsicStatus
     * @request GET:/api/v1/hashboards/{hb_sn}/{asic_id}
     */
    getAsicStatus: ({ hbSn, asicId }: GetAsicStatusParams, params: RequestParams = {}) =>
      this.request<AsicStatsResponse, MessageResponse>({
        path: `/api/v1/hashboards/${hbSn}/${asicId}`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The hashrate endpoint provides miner-level historical hashrate operation data.
     *
     * @tags Hashrate
     * @name GetMinerHashrate
     * @request GET:/api/v1/hashrate
     */
    getMinerHashrate: (query: GetMinerHashrateParams, params: RequestParams = {}) =>
      this.request<HashrateResponse, MessageResponse>({
        path: `/api/v1/hashrate`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The hashrate endpoint provides hashboard-level historical operation data.
     *
     * @tags Hashrate
     * @name GetHashboardHashrate
     * @request GET:/api/v1/hashrate/{hb_sn}
     */
    getHashboardHashrate: ({ hbSn, ...query }: GetHashboardHashrateParams, params: RequestParams = {}) =>
      this.request<HashrateResponse, MessageResponse>({
        path: `/api/v1/hashrate/${hbSn}`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The hashrate endpoint provides ASIC-level historical hashrate operation data.
     *
     * @tags Hashrate
     * @name GetAsicHashrate
     * @request GET:/api/v1/hashrate/{hb_sn}/{asic_id}
     */
    getAsicHashrate: ({ hbSn, asicId, ...query }: GetAsicHashrateParams, params: RequestParams = {}) =>
      this.request<HashrateResponse, MessageResponse>({
        path: `/api/v1/hashrate/${hbSn}/${asicId}`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The temperature endpoint provides miner-level historical temperature operation data.
     *
     * @tags Temperature
     * @name GetMinerTemperature
     * @request GET:/api/v1/temperature
     */
    getMinerTemperature: (query: GetMinerTemperatureParams, params: RequestParams = {}) =>
      this.request<TemperatureResponse, MessageResponse>({
        path: `/api/v1/temperature`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The temperature endpoint provides hashboard-level historical operation data.
     *
     * @tags Temperature
     * @name GetHashboardTemperature
     * @request GET:/api/v1/temperature/{hb_sn}
     */
    getHashboardTemperature: ({ hbSn, ...query }: GetHashboardTemperatureParams, params: RequestParams = {}) =>
      this.request<TemperatureResponse, MessageResponse>({
        path: `/api/v1/temperature/${hbSn}`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The hashrate endpoint provides ASIC-level historical temperature operation data.
     *
     * @tags Temperature
     * @name GetAsicTemperature
     * @request GET:/api/v1/temperature/{hb_sn}/{asic_id}
     */
    getAsicTemperature: ({ hbSn, asicId, ...query }: GetAsicTemperatureParams, params: RequestParams = {}) =>
      this.request<TemperatureResponse, MessageResponse>({
        path: `/api/v1/temperature/${hbSn}/${asicId}`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The power endpoint provides miner-level historical power operation data.
     *
     * @tags Power
     * @name GetMinerPower
     * @request GET:/api/v1/power
     */
    getMinerPower: (query: GetMinerPowerParams, params: RequestParams = {}) =>
      this.request<PowerResponse, MessageResponse>({
        path: `/api/v1/power`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The hardware endpoint provides information about the hardware components of the miner. This includes hashboards, power supplies, and fans.
     *
     * @tags Hardware, PSUs, Hashboards, Fans
     * @name GetHardware
     * @request GET:/api/v1/hardware
     */
    getHardware: (params: RequestParams = {}) =>
      this.request<HardwareInfo, MessageResponse>({
        path: `/api/v1/hardware`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The get power supplies endpoint returns the full list of currently configured power supplies.
     *
     * @tags PSUs
     * @name ListPowerSupplies
     * @request GET:/api/v1/hardware/psus
     */
    listPowerSupplies: (params: RequestParams = {}) =>
      this.request<PsusInfo, MessageResponse>({
        path: `/api/v1/hardware/psus`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description Returns information about all PSUs including firmware update status and available firmware updates.
     *
     * @tags PSUs
     * @name GetPowerSupplies
     * @request GET:/api/v1/power-supplies
     */
    getPowerSupplies: (params: RequestParams = {}) =>
      this.request<PowerSuppliesResponse, MessageResponse>({
        path: `/api/v1/power-supplies`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description Triggers a PSU firmware update. Publishes a firmware update command to each connected PSU via NATS. Use the `force` parameter to allow re-flashing the same firmware version.
     *
     * @tags PSUs
     * @name PostUpdatePsu
     * @request POST:/api/v1/power-supplies/update
     * @secure
     */
    postUpdatePsu: (query: PostUpdatePsuParams, params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/power-supplies/update`,
        method: "POST",
        query: query,
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * @description The power endpoint provides hashboard-level historical operation data.
     *
     * @tags Power
     * @name GetHashboardPower
     * @request GET:/api/v1/power/{hb_sn}
     */
    getHashboardPower: ({ hbSn, ...query }: GetHashboardPowerParams, params: RequestParams = {}) =>
      this.request<PowerResponse, MessageResponse>({
        path: `/api/v1/power/${hbSn}`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The efficiency endpoint provides miner-level historical power operation data.
     *
     * @tags Efficiency
     * @name GetMinerEfficiency
     * @request GET:/api/v1/efficiency
     */
    getMinerEfficiency: (query: GetMinerEfficiencyParams, params: RequestParams = {}) =>
      this.request<EfficiencyResponse, MessageResponse>({
        path: `/api/v1/efficiency`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The efficiency endpoint provides hashboard-level historical operation data.
     *
     * @tags Efficiency
     * @name GetHashboardEfficiency
     * @request GET:/api/v1/efficiency/{hb_sn}
     */
    getHashboardEfficiency: ({ hbSn, ...query }: GetHashboardEfficiencyParams, params: RequestParams = {}) =>
      this.request<EfficiencyResponse, MessageResponse>({
        path: `/api/v1/efficiency/${hbSn}`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description The cooling endpoint provides information on the cooling status of the device, including mode, current fan RPM, and target temperature.
     *
     * @tags Cooling
     * @name GetCooling
     * @request GET:/api/v1/cooling
     */
    getCooling: (params: RequestParams = {}) =>
      this.request<CoolingStatus, MessageResponse>({
        path: `/api/v1/cooling`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The cooling configuration endpoint allows the user to control the fan mode, speed percentage (for Manual mode), and target temperature (for Auto mode).
     *
     * @tags Cooling
     * @name SetCoolingMode
     * @request PUT:/api/v1/cooling
     * @secure
     */
    setCoolingMode: (data: CoolingConfig, params: RequestParams = {}) =>
      this.request<CoolingConfig, MessageResponse | ErrorResponse>({
        path: `/api/v1/cooling`,
        method: "PUT",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description The network GET endpoint provides information related to the network configuration of the miner including IP address, gateways, and MAC address.
     *
     * @tags Network
     * @name GetNetwork
     * @request GET:/api/v1/network
     */
    getNetwork: (params: RequestParams = {}) =>
      this.request<NetworkInfo, MessageResponse>({
        path: `/api/v1/network`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description The network PUT endpoint allows the user to change the configuration of the miner between DHCP and a static IP.
     *
     * @tags Network
     * @name SetNetworkConfig
     * @request PUT:/api/v1/network
     * @secure
     */
    setNetworkConfig: (data: NetworkConfig, params: RequestParams = {}) =>
      this.request<NetworkInfo, MessageResponse>({
        path: `/api/v1/network`,
        method: "PUT",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description The errors endpoint provides alerts to be surfaced on the UI with different severity levels such as errors or warnings. This endpoint should be polled periodically to surface any issues that arise during mining operation.
     *
     * @tags Errors
     * @name GetErrors
     * @request GET:/api/v1/errors
     */
    getErrors: (params: RequestParams = {}) =>
      this.request<ErrorListResponse, MessageResponse>({
        path: `/api/v1/errors`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description Retrieve the current system tag value.
     *
     * @tags System Tag
     * @name GetSystemTag
     * @request GET:/api/v1/system/tag
     */
    getSystemTag: (params: RequestParams = {}) =>
      this.request<string | number | boolean | object | any[], MessageResponse>({
        path: `/api/v1/system/tag`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description Set or update the system tag value. Accepts any non-null JSON value (string, number, boolean, object, or array). Maximum size is 10 KiB when serialized.
     *
     * @tags System Tag
     * @name PutSystemTag
     * @request PUT:/api/v1/system/tag
     * @secure
     */
    putSystemTag: (data: string | number | boolean | object | any[], params: RequestParams = {}) =>
      this.request<MessageResponse, MessageResponse>({
        path: `/api/v1/system/tag`,
        method: "PUT",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description Remove the current system tag.
     *
     * @tags System Tag
     * @name DeleteSystemTag
     * @request DELETE:/api/v1/system/tag
     * @secure
     */
    deleteSystemTag: (params: RequestParams = {}) =>
      this.request<void, MessageResponse>({
        path: `/api/v1/system/tag`,
        method: "DELETE",
        secure: true,
        ...params,
      }),

    /**
     * @description Get the current system telemetry enabled status.
     *
     * @tags System
     * @name GetSystemTelemetryEnabled
     * @request GET:/api/v1/system/telemetry
     */
    getSystemTelemetryEnabled: (params: RequestParams = {}) =>
      this.request<TelemetryResponse, TelemetryResponse>({
        path: `/api/v1/system/telemetry`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description Configure system telemetry enabled settings.
     *
     * @tags System
     * @name SetSystemTelemetryEnabled
     * @request PUT:/api/v1/system/telemetry
     */
    setSystemTelemetryEnabled: (data: TelemetryConfig, params: RequestParams = {}) =>
      this.request<TelemetryResponse, MessageResponse | TelemetryResponse>({
        path: `/api/v1/system/telemetry`,
        method: "PUT",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description The time series endpoint provides unified access to historical data for multiple metrics and levels. It allows querying hashrate, temperature, power, and efficiency data for miner, hashboard, ASIC, and PSU levels in a single request with flexible time ranges and aggregation options.
     *
     * @tags Time Series
     * @name GetTimeSeries
     * @request POST:/api/v1/timeseries
     */
    getTimeSeries: (data: TimeSeriesRequest, params: RequestParams = {}) =>
      this.request<TimeSeriesResponse, MessageResponse>({
        path: `/api/v1/timeseries`,
        method: "POST",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description Returns current telemetry values. The 'level' parameter is a comma-separated list that controls which data types are included. If not specified, defaults to 'miner'. Note: 'asic' implicitly includes 'hashboard' since ASIC data is nested within hashboards. Examples: no level param (miner only), ?level=hashboard (hashboards only), ?level=asic (hashboards with ASIC data), ?level=miner,asic,psu (all data).
     *
     * @tags Telemetry
     * @name GetCurrentTelemetry
     * @summary Get current telemetry data
     * @request GET:/api/v1/telemetry
     */
    getCurrentTelemetry: (query: GetCurrentTelemetryParams, params: RequestParams = {}) =>
      this.request<TelemetryData, MessageResponse>({
        path: `/api/v1/telemetry`,
        method: "GET",
        query: query,
        format: "json",
        ...params,
      }),

    /**
     * @description Get pairing information including MAC address and serial number. This endpoint does not require authentication.
     *
     * @tags Pairing
     * @name GetPairingInfo
     * @request GET:/api/v1/pairing/info
     */
    getPairingInfo: (params: RequestParams = {}) =>
      this.request<PairingInfoResponse, any>({
        path: `/api/v1/pairing/info`,
        method: "GET",
        format: "json",
        ...params,
      }),

    /**
     * @description Set the authentication public key for pairing. On first pair this endpoint does not require authentication. On key rotation, authentication is required.
     *
     * @tags Pairing
     * @name SetAuthKey
     * @request POST:/api/v1/pairing/auth-key
     */
    setAuthKey: (data: SetAuthKeyRequest, params: RequestParams = {}) =>
      this.request<SetAuthKeyResponse, ErrorResponse>({
        path: `/api/v1/pairing/auth-key`,
        method: "POST",
        body: data,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * @description Clear the authentication public key. This endpoint requires authentication.
     *
     * @tags Pairing
     * @name ClearAuthKey
     * @request DELETE:/api/v1/pairing/auth-key
     * @secure
     */
    clearAuthKey: (params: RequestParams = {}) =>
      this.request<MessageResponse, ErrorResponse>({
        path: `/api/v1/pairing/auth-key`,
        method: "DELETE",
        secure: true,
        format: "json",
        ...params,
      }),
  };
}
