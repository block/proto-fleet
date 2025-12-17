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
  useSetTheme,
  useSetDeviceTheme,
  useSetTemperatureUnit,
  useSetDuration,
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
  useMinerDeviceStatus,
  useMinerHashrate,
  useMinerEfficiency,
  useMinerPowerUsage,
  useMinerTemperature,
  useMinerUrl,
  useDeviceErrors,
  useMinerData,
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
} from "./hooks/useFleet";

// =============================================================================
// Onboarding Hooks
// =============================================================================

export {
  usePoolConfigured,
  useDevicePaired,
  useOnboardingComplete,
  useSetOnboardingStatus,
  useSetPoolConfigured,
  useSetDevicePaired,
} from "./hooks/useOnboarding";

// =============================================================================
// Dashboard Hooks
// =============================================================================

export {
  usePanelMetrics,
  useTemperatureStatusCounts,
  useUptimeStatusCounts,
  useDashboardError,
  useSetHistoricalMetrics,
  useAppendStreamingMetrics,
  useSetHistoricalTemperatureCounts,
  useAppendStreamingTemperatureCounts,
  useSetHistoricalUptimeCounts,
  useAppendStreamingUptimeCounts,
  useSetAllHistoricalData,
  useClearMetrics,
  useSetDashboardError,
} from "./hooks/useDashboard";

// =============================================================================
// Types
// =============================================================================

export type { Theme, ThemeColor, TemperatureUnit } from "@/shared/features/preferences";
export type { MinerStateSnapshot } from "./slices/fleetSlice";
