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
  reset: () => void;
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

  // setSystemInfo(undefined) is a no-op by design (it only merges truthy
  // values), so a real clear must remove the flattened API fields. Delete every
  // data field (everything that isn't an action) so a miner switch fully clears
  // the previous device's system info.
  reset: () =>
    set((state) => {
      const slice = state.systemInfo as unknown as Record<string, unknown>;
      for (const key of Object.keys(slice)) {
        if (typeof slice[key] !== "function") {
          delete slice[key];
        }
      }
    }),
});
