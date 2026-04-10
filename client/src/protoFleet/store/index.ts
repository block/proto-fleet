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
// Batch Hooks
// =============================================================================

export {
  useBatchStateVersion,
  useStartBatchOperation,
  useCompleteBatchOperation,
  useRemoveDevicesFromBatch,
  useCleanupStaleBatches,
  getActiveBatches,
  getAllBatches,
} from "./hooks/useBatch";

export type { BatchOperation, BatchOperationInput } from "./slices/batchSlice";

// =============================================================================
// UI Hooks
// =============================================================================

export {
  useTheme,
  useDeviceTheme,
  useTemperatureUnit,
  useDuration,
  useBulkRenamePreferences,
  useIsActionBarVisible,
  useSetTheme,
  useSetDeviceTheme,
  useSetTemperatureUnit,
  useSetDuration,
  useSetBulkRenamePreferences,
  useSetActionBarVisible,
} from "./hooks/useUI";

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
