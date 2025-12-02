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
  useMinerComponentStatus,
  useMinerDeviceStatus,
  useMinerHashrate,
  useMinerEfficiency,
  useMinerPowerUsage,
  useMinerTemperature,
  useMinerUrl,
  useSetMiners,
  useAppendMiners,
  useSetTotalMiners,
  useSetDeviceStatusCounts,
  useSetRefetchCallback,
  useUpdateMinerMeasurement,
  useUpdateMinerTelemetry,
  useUpdateMinerComponentStatus,
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
// Types
// =============================================================================

export type { Theme, ThemeColor, TemperatureUnit } from "@/shared/features/preferences";
