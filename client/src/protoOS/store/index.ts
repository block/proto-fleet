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
  useFirmwareUpdateDismissed,
  useSetFirmwareUpdateDismissed,
  useShowLoginModal,
  useDismissedLoginModal,
  usePausedAuthAction,
  useSetShowLoginModal,
  useSetDismissedLoginModal,
  useSetPausedAuthAction,
} from "./hooks/useUI";

export type { Theme, ThemeColor, TemperatureUnit } from "./types";

export {
  useMinerHardware,
  useHashboardsHardware,
  useHashboardSerials,
  useHashboardSerialsByBay,
  useHashboardHardware,
  useHashboardsByBay,
  useBayCount,
  useSlotsPerBay,
  useHashboardSlot,
  useHashboardBay,
  useAsicRowsByHbSn,
  useAsicHardware,
  useAsicPosition,
  useAsicsByHashboard,
  useControlBoard,
  usePsus,
  usePsuIds,
  usePsu,
  useFans,
  useFanIds,
  useFan,
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

export {
  useSystemInfo,
  useProductName,
  useSerialNumber,
  useOSVersion,
  useFwUpdateStatus,
  useSystemInfoPending,
  useSystemInfoError,
  useIsProtoRig,
  useIsWebServerRunning,
  useIsMiningDriverRunning,
  useHasFirmwareUpdate,
  useFirmwareUpdateInstalling,
  useSetSystemInfo,
  useSetSystemInfoError,
  useSetSystemInfoPending,
} from "./hooks/useSystemInfo";

export {
  useNetworkInfo,
  useHostname,
  useIpAddress,
  useMacAddress,
  useGateway,
  useNetmask,
  useDhcp,
  useNetworkInfoPending,
  useNetworkInfoError,
  useSetNetworkInfo,
  useSetNetworkInfoError,
  useSetNetworkInfoPending,
} from "./hooks/useNetworkInfo";

export type { MiningStatus } from "./slices/minerStatusSlice";

export {
  useAuthTokens,
  useRefreshToken,
  useAuthLoading,
  useSetAuthTokens,
  useSetAuthLoading,
  useLogout,
  useAuthHeader,
  useAuthErrors,
  useAccessToken,
} from "./hooks/useAuth";

export type { AuthTokens } from "./slices/authSlice";
export { AUTH_ACTIONS } from "./types";
export type { AuthAction } from "./types";

export {
  useMiningTargetValue,
  useMiningTargetDefault,
  useMiningTargetPerformanceMode,
  useMiningTargetBounds,
  useMiningTargetPending,
  useMiningTargetError,
  useSetMiningTargetValue,
  useSetMiningTargetDefault,
  useSetMiningTargetPerformanceMode,
  useSetMiningTargetBounds,
  useSetMiningTargetPending,
  useSetMiningTargetError,
  useSetMiningTargetFromResponse,
  useResetMiningTarget,
} from "./hooks/useMiningTarget";
