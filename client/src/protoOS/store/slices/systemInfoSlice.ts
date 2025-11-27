import type { StateCreator } from "zustand";
import type { MinerStore } from "../useMinerStore";
import type { SystemInfoSysteminfo } from "@/protoOS/api/generatedApi";

// =============================================================================
// Slice Interface
// =============================================================================

export interface SystemInfoSlice extends SystemInfoSysteminfo {
  // Request state
  pending?: boolean;
  error?: string;

  // Actions
  setSystemInfo: (systemInfo: SystemInfoSysteminfo | undefined) => void;
  setError: (error: string | undefined) => void;
  setPending: (pending: boolean) => void;
}

// =============================================================================
// Slice Creator
// =============================================================================

export const createSystemInfoSlice: StateCreator<MinerStore, [["zustand/immer", never]], [], SystemInfoSlice> = (
  set,
) => ({
  // Actions
  setSystemInfo: (systemInfo) =>
    set((state) => {
      if (systemInfo) {
        // Spread all fields from systemInfo into the root level
        Object.assign(state.systemInfo, systemInfo);
      }
      // Note: When systemInfo is undefined, we don't clear fields -
      // they remain as is. To clear, pass an empty object {} instead.
    }),

  setError: (error) =>
    set((state) => {
      state.systemInfo.error = error;
      state.systemInfo.pending = false;
    }),

  setPending: (pending) =>
    set((state) => {
      state.systemInfo.pending = pending;
    }),
});
