// =============================================================================
// Main Store Export
// =============================================================================

export { useFleetStore } from "./useFleetStore";
export type { FleetStore } from "./useFleetStore";

// =============================================================================
// Auth Hooks
// =============================================================================

export {
  useAuthTokens,
  useAccessToken,
  useUsername,
  useAuthLoading,
  useSetAuthTokens,
  useSetUsername,
  useSetAuthLoading,
  useLogout,
  useAuthHeader,
  useAuthErrors,
} from "./hooks/useAuth";

export { getAuthHeader, useIsAuthenticated } from "./hooks/useAuthentication";

// =============================================================================
// UI Hooks
// =============================================================================

export {
  useTheme,
  useDeviceTheme,
  useTemperatureUnit,
  useSetTheme,
  useSetDeviceTheme,
  useSetTemperatureUnit,
} from "./hooks/useUI";

// =============================================================================
// Fleet Hooks
// =============================================================================

export {
  useMiner,
  useMinerIds,
  useTotalMiners,
  useMinerStateCounts,
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
  useSetMinerStateCounts,
  useHandleMinerStateCountsChange,
  useSetCurrentFilter,
  useSetRefetchCallback,
  useUpdateMinerMeasurement,
  useUpdateMinerTelemetry,
  useUpdateMinerComponentStatus,
  useUpdateMinerDeviceStatus,
  useUpdateMinerTimestamp,
  useSetLoading,
  useSetStreaming,
  useSetCursor,
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

export type { AuthTokens } from "./slices/authSlice";
export type {
  Theme,
  ThemeColor,
  TemperatureUnit,
} from "@/shared/features/preferences";
