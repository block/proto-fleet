import { useFleetStore } from "../useFleetStore";

// =============================================================================
// Onboarding State Selectors
// =============================================================================

export const usePoolConfigured = () => useFleetStore((state) => state.onboarding.poolConfigured);

export const useDevicePaired = () => useFleetStore((state) => state.onboarding.devicePaired);

export const useOnboardingStatusLoaded = () => useFleetStore((state) => state.onboarding.statusLoaded);

export const useOnboardingComplete = () =>
  useFleetStore((state) => state.onboarding.devicePaired === true && state.onboarding.poolConfigured === true);

// =============================================================================
// Onboarding Action Selectors
// =============================================================================

export const useSetOnboardingStatus = () => useFleetStore((state) => state.onboarding.setStatus);

export const useSetPoolConfigured = () => useFleetStore((state) => state.onboarding.setPoolConfigured);

export const useSetDevicePaired = () => useFleetStore((state) => state.onboarding.setDevicePaired);

export const useResetOnboardingStatus = () => useFleetStore((state) => state.onboarding.resetStatus);
