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
});
