import type { StateCreator } from "zustand";
import type { NetworkInfoState, NetworkInterface, ResourceState } from "../contract";
import type { MinerStore } from "../useMinerStore";

export interface NetworkInfoSlice extends NetworkInfoState {
  setNetworkInfo: (interfaces: Map<string, NetworkInterface> | undefined) => void;
  setError: (error: string | undefined) => void;
  setPending: (pending: boolean) => void;
  reset: () => void;
}

const idleResource = (): ResourceState => ({ status: "idle" });

export const createNetworkInfoSlice: StateCreator<MinerStore, [["zustand/immer", never]], [], NetworkInfoSlice> = (
  set,
) => ({
  resource: idleResource(),
  interfaces: new Map(),

  setNetworkInfo: (interfaces) =>
    set((state) => {
      state.networkInfo.interfaces = interfaces ?? new Map();
      state.networkInfo.resource = interfaces ? { status: "ready", updatedAtMs: Date.now() } : idleResource();
    }),

  setError: (error) =>
    set((state) => {
      state.networkInfo.resource = error
        ? { status: "error", error: { code: "NETWORK_INFO", message: error, retryable: true } }
        : idleResource();
    }),

  setPending: (pending) =>
    set((state) => {
      if (pending) state.networkInfo.resource = { status: "loading" };
    }),

  reset: () =>
    set((state) => {
      state.networkInfo.interfaces = new Map();
      state.networkInfo.resource = idleResource();
    }),
});
