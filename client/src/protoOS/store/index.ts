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
export { convertValueUnits, formatValue, convertAndFormatMeasurement } from "./utils/telemetryUtils";

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
  useAsicDataTransform,
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
  useCoolingMode,
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
  useOnboarded,
  usePasswordSet,
  useDefaultPasswordActive,
  useSystemStatus,
  useWakeDialog,
  useSetMiningStatus,
  useSetErrors,
  useSetOnboarded,
  useSetPasswordSet,
  useSetDefaultPasswordActive,
  useSetSystemStatus,
  useShowWakeDialog,
  useHideWakeDialog,
  // Error selector hooks
  useGroupedErrors,
  useErrorsByComponent,
  useErrors,
  useHasIssues,
} from "./hooks/useMinerStatus";

// Pools hooks
export { usePoolsInfo, useSetPoolsInfo } from "./hooks/usePools";

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

// System status hooks removed - now in minerStatus
// useOnboarded and usePasswordSet are exported from useMinerStatus above

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
export type { MinerError, ErrorSource } from "./types";

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

export { useAuthRetry } from "./hooks/useAuthRetry";

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
