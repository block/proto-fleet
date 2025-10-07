// =============================================================================
// Miner Store - Clean Public API
// =============================================================================

// Main store
export { default as useMinerStore } from "./useMinerStore";

// Types (only export what consumers need)
export type {
  ChartDataPoint,
  Measurement,
  MetricUnit,
  AsicHardwareData,
  HashboardHardwareData,
  MinerHardwareData,
  AsicTelemetryData,
  HashboardTelemetryData,
  MinerTelemetryData,
  AsicData,
  HashboardData,
  MinerData,
  MetricTimeSeries,
} from "./types";

// Utilities
export {
  convertValueUnits,
  formatValue,
  convertAndFormatMeasurement,
} from "./utils/telemetryUtils";

export { getAsicId } from "./utils/getAsicId";
export { getAsicName } from "./utils/getAsicName";

// Convenience hooks
export {
  useMiner,
  useMinerHashboard,
  useMinerHashboards,
  useMinerAsic,
  useMinerHashboardAsics,
  useChartDataForMetric,
} from "./hooks/useMiner";

export {
  useMinerTelemetry,
  useHashboardsTelemetry,
  useHashboardTelemetry,
  useAsicsTelemetry,
  useAsicTelemetry,
  useIntervalMs,
} from "./hooks/useTelemetry";

export {
  useDuration,
  useSetDuration,
  useActiveChartLines,
  useSetActiveChartLines,
  useToggleActiveChartLine,
} from "./hooks/useUI";

export {
  useMinerHardware,
  useHashboardsHardware,
  useHashboardSerials,
  useHashboardHardware,
  useHashboardsByBay,
  useBayCount,
  useHashboardSlot,
  useHashboardBay,
  useHashboardBaySlotIndex,
  useAsicRowsByHbSn,
  useAsicHardware,
  useAsicPosition,
  useAsicsByHashboard,
} from "./hooks/useHardware";
