import type { StateCreator } from "zustand";
import type { MinerStore } from "../useMinerStore";
import type { NetworkInfoNetworkinfo } from "@/protoOS/api/generatedApi";

// =============================================================================
// Slice Interface
// =============================================================================

export interface NetworkInfoSlice extends NetworkInfoNetworkinfo {
  // Request state
  pending?: boolean;
  error?: string;

  // Actions
  setNetworkInfo: (networkInfo: NetworkInfoNetworkinfo | undefined) => void;
  setError: (error: string | undefined) => void;
  setPending: (pending: boolean) => void;
  reset: () => void;
}

// =============================================================================
// Slice Creator
// =============================================================================

export const createNetworkInfoSlice: StateCreator<MinerStore, [["zustand/immer", never]], [], NetworkInfoSlice> = (
  set,
) => ({
  // Actions
  setNetworkInfo: (networkInfo) =>
    set((state) => {
      if (networkInfo) {
        Object.assign(state.networkInfo, networkInfo);
      }
    }),

  setError: (error) =>
    set((state) => {
      state.networkInfo.error = error;
      state.networkInfo.pending = false;
    }),

  setPending: (pending) =>
    set((state) => {
      state.networkInfo.pending = pending;
    }),

  // setNetworkInfo(undefined) is a no-op by design (it only merges truthy
  // values), so a real clear must remove the flattened API fields. Delete every
  // data field (everything that isn't an action) so a miner switch fully clears
  // the previous device's network info (IP/MAC/etc.).
  reset: () =>
    set((state) => {
      const slice = state.networkInfo as unknown as Record<string, unknown>;
      for (const key of Object.keys(slice)) {
        if (typeof slice[key] !== "function") {
          delete slice[key];
        }
      }
    }),
});
