import type { StateCreator } from "zustand";
import type { MinerStore } from "../useMinerStore";

// =============================================================================
// Slice Interface
// =============================================================================

export interface SystemStatus {
  onboarded?: boolean;
  passwordSet?: boolean;
}

export interface SystemStatusSlice {
  onboarded?: boolean;
  passwordSet?: boolean;

  // Actions
  // Accepts both API format (snake_case) and internal format (camelCase)
  setSystemStatus: (systemStatus: SystemStatus | undefined) => void;
}

// =============================================================================
// Slice Creator
// =============================================================================

export const createSystemStatusSlice: StateCreator<
  MinerStore,
  [["zustand/immer", never]],
  [],
  SystemStatusSlice
> = (set) => ({
  // Initial State
  onboarded: undefined,
  passwordSet: undefined,

  // Actions
  setSystemStatus: (systemStatus) =>
    set((state) => {
      if (systemStatus?.onboarded !== undefined) {
        state.systemStatus.onboarded = systemStatus.onboarded;
      }
      if (systemStatus?.passwordSet !== undefined) {
        state.systemStatus.passwordSet = systemStatus.passwordSet;
      }
    }),
});
