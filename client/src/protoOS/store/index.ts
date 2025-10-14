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
  ControlBoardHardwareData,
  AsicTelemetryData,
  HashboardTelemetryData,
  MinerTelemetryData,
  AsicData,
  HashboardData,
  MinerData,
  MetricTimeSeries,
  PsuHardwareData,
  PsuTelemetryData,
  PsuData,
  FanHardwareData,
  FanTelemetryData,
  FanData,
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
  useMinerPsu,
  useMinerPsus,
  useMinerFan,
  useMinerFans,
  useChartDataForMetric,
} from "./hooks/useMiner";

export {
  useMinerTelemetry,
  useHashboardsTelemetry,
  useHashboardTelemetry,
  useAsicsTelemetry,
  useAsicTelemetry,
  usePsusTelemetry,
  usePsuTelemetry,
  useFansTelemetry,
  useFanTelemetry,
  useIntervalMs,
} from "./hooks/useTelemetry";

export {
  useDuration,
  useSetDuration,
  useActiveChartLines,
  useSetActiveChartLines,
  useToggleActiveChartLine,
  useTheme,
  useDeviceTheme,
  useSetTheme,
  useSetDeviceTheme,
  useTemperatureUnit,
  useSetTemperatureUnit,
} from "./hooks/useUI";

export type { Theme, ThemeColor, TemperatureUnit } from "./types";

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

export {
  useMiningStatus,
  useMiningUptime,
  useRebootUptime,
  useHwErrors,
  useMiningStatusMessage,
  useIsWarmingUp,
  useIsSleeping,
  useIsMining,
  useIsAwake,
  useMinerErrors,
  usePoolsInfo,
  useWakeDialog,
  useComprehensiveStatus,
  useSetMiningStatus,
  useSetErrors,
  useSetPoolsInfo,
  useShowWakeDialog,
  useHideWakeDialog,
} from "./hooks/useMinerStatus";

export type { MiningStatus } from "./slices/minerStatusSlice";
