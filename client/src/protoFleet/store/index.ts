// =============================================================================
// Main Store Export
// =============================================================================

export { useFleetStore } from "./useFleetStore";
export type { FleetStore } from "./useFleetStore";

// =============================================================================
// Auth Hooks
// =============================================================================

export {
  useSessionExpiry,
  useIsAuthenticated,
  useUsername,
  useRole,
  useAuthLoading,
  useTemporaryPassword,
  useSetSessionExpiry,
  useSetIsAuthenticated,
  useSetUsername,
  useSetRole,
  useSetAuthLoading,
  useSetTemporaryPassword,
  useLogout,
  useAuthErrors,
} from "./hooks/useAuth";

export { useCheckAuthentication } from "./hooks/useAuthentication";

// =============================================================================
// UI Hooks
// =============================================================================

export {
  useTheme,
  useDeviceTheme,
  useTemperatureUnit,
  useDuration,
  useVisibleMinerIds,
  useSetTheme,
  useSetDeviceTheme,
  useSetTemperatureUnit,
  useSetDuration,
  useSetVisibleMinerIds,
} from "./hooks/useUI";

// =============================================================================
// Fleet Hooks
// =============================================================================

export {
  useMiner,
  useMinerIds,
  useTotalMiners,
  useDeviceStatusCounts,
  useFleetMiners,
  useIsLoading,
  useIsStreaming,
  useMinerName,
  useMinerMacAddress,
  useMinerIpAddress,
  useMinerModel,
  useMinerFirmwareVersion,
  useMinerDeviceStatus,
  useMinerHashrate,
  useMinerEfficiency,
  useMinerPowerUsage,
  useMinerTemperature,
  useMinerUrl,
  useDeviceErrors,
  useMinerData,
  useMinerActiveBatches,
  useSetMiners,
  useAppendMiners,
  useSetTotalMiners,
  useSetDeviceStatusCounts,
  useSetRefetchCallback,
  useUpdateMinerMeasurement,
  useUpdateMinerTelemetry,
  useUpdateMinerDeviceStatus,
  useUpdateMinerTimestamp,
  useSetLoading,
  useSetStreaming,
  useSetCursor,
  useLastPairingCompletedAt,
  useNotifyPairingCompleted,
  useStartBatchOperation,
  useCompleteBatchOperation,
  useRemoveDevicesFromBatch,
  useCleanupStaleBatches,
  useBatchOperationCount,
} from "./hooks/useFleet";

// =============================================================================
// Onboarding Hooks
// =============================================================================

export {
  usePoolConfigured,
  useDevicePaired,
  useOnboardingStatusLoaded,
  useOnboardingComplete,
  useSetOnboardingStatus,
  useSetPoolConfigured,
  useSetDevicePaired,
  useResetOnboardingStatus,
} from "./hooks/useOnboarding";

// =============================================================================
// Dashboard Hooks
// =============================================================================

export {
  usePanelMetrics,
  useTemperatureStatusCounts,
  useUptimeStatusCounts,
  useMinerStateCounts,
  useDashboardError,
  useSetHistoricalMetrics,
  useAppendStreamingMetrics,
  useSetHistoricalTemperatureCounts,
  useAppendStreamingTemperatureCounts,
  useSetHistoricalUptimeCounts,
  useAppendStreamingUptimeCounts,
  useSetAllHistoricalData,
  useSetMinerStateCounts,
  useClearMetrics,
  useSetDashboardError,
} from "./hooks/useDashboard";

// =============================================================================
// Types
// =============================================================================

export type { Theme, ThemeColor, TemperatureUnit } from "@/shared/features/preferences";
export type { MinerStateSnapshot } from "./slices/fleetSlice";
