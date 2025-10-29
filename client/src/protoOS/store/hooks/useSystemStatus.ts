import { useShallow } from "zustand/react/shallow";
import useMinerStore from "../useMinerStore";

// =============================================================================
// Granular Hooks
// =============================================================================

/**
 * Hook to get all system status data
 * Returns the entire systemStatus slice
 */
export const useSystemStatus = () =>
  useMinerStore(useShallow((state) => state.systemStatus));

/**
 * Hook to get specific system status fields
 */
export const useOnboarded = () =>
  useMinerStore((state) => state.systemStatus.onboarded);

export const usePasswordSet = () =>
  useMinerStore((state) => state.systemStatus.passwordSet);

// =============================================================================
// Action Hooks
// =============================================================================

/**
 * Hook to get the setSystemStatus action
 */
export const useSetSystemStatus = () =>
  useMinerStore((state) => state.systemStatus.setSystemStatus);
