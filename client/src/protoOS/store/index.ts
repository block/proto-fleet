export { default as useMinerStore } from "./useMinerStore";

export type {
  ChartDataPoint,
  Measurement,
  DisplayMetricUnit,
  AsicHardwareData,
  HashboardHardwareData,
  MinerHardwareData,
  ControlBoardHardwareData,
  AsicData,
  HashboardData,
  MinerData,
  PsuHardwareData,
  PsuData,
  FanHardwareData,
  FanData,
} from "./types";

export type { MetricTimeSeries, MetricTelemetry } from "./contract";

export {
  convertValueUnits,
  formatValue,
  convertAndFormatMeasurement,
  temperatureDisplayUnit,
} from "./utils/telemetryUtils";

export { getAsicId } from "./utils/getAsicId";
export { getFanId, getHashboardId, getPsuId } from "./utils/entityIds";
export { getAsicName } from "./utils/getAsicName";
export {
  getContainerAsicLabel,
  CONTAINER_HASHBOARD_COLUMNS,
  CONTAINER_HASHBOARD_ROWS,
  CONTAINER_HASHBOARD_ROW_LETTERS,
} from "./utils/containerHashboardLayout";

export {
  useMiner,
  useMinerHashboard,
  useMinerHashboards,
  useMinerAsic,
  useMinerHashboardAsics,
  useMinerPsu,
  useMinerPsuById,
  useMinerPsus,
  useMinerFan,
  useMinerFans,
  useChartDataForMetric,
  useAsicDataTransform,
} from "./hooks/useMiner";
export type { KpiChartSeries, KpiMetricName } from "./hooks/kpiChartData";

export {
  useHashboardsTelemetry,
  useHashboardTelemetry,
  useAsicsTelemetry,
  useAsicTelemetry,
  usePsusTelemetry,
  usePsuTelemetry,
  useFansTelemetry,
  useFanTelemetry,
  useCoolingMode,
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
  useFlowUnit,
  useSetFlowUnit,
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
  useGroupedErrors,
  useErrorsByComponent,
  useErrors,
  useHasIssues,
} from "./hooks/useMinerStatus";

export { usePoolsInfo, useSetPoolsInfo } from "./hooks/usePools";

export {
  useSystemInfo,
  useProductName,
  useSerialNumber,
  useOSVersion,
  useFwUpdateStatus,
  useSystemTagValue,
  useSystemInfoPending,
  useSystemInfoError,
  useIsProtoRig,
  useDeviceType,
  useModuleModel,
  useIsWebServerRunning,
  useIsMiningDriverRunning,
  useHasFirmwareUpdate,
  useFirmwareUpdateInstalling,
  useSetSystemInfo,
  useSetSystemTag,
  useSetSystemInfoError,
  useSetSystemInfoPending,
} from "./hooks/useSystemInfo";

export type { DeviceType } from "./slices/systemInfoSlice";

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

export type { MiningStatus } from "./contract";
export type { MinerError, ErrorSource } from "./types";

export {
  useAuthTokens,
  useRefreshToken,
  useAuthLoading,
  useSetAuthTokens,
  useSetAuthLoading,
  useLogout,
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
  useMiningTargetStatus,
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
