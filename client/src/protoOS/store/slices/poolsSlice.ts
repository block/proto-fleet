import type { StateCreator } from "zustand";
import { type PoolInfo, type PoolsState, type ResourceState, ROOT_MODULE_KEY } from "../contract";
import type { MinerStore } from "../useMinerStore";
import type { PoolConfigurationStatisticsService } from "@/protoOS/api/adapters/poolStatistics";

export type PoolConfigurationMode = boolean | "unavailable";

export interface PoolsSlice extends PoolsState {
  configurationMode?: { service: PoolConfigurationStatisticsService; value: PoolConfigurationMode };
  setConfigurationMode: (service: PoolConfigurationStatisticsService, value: PoolConfigurationMode) => void;
  setPoolsInfo: (poolsInfo: PoolInfo[] | undefined) => void;
  setPending: (pending: boolean) => void;
  setError: (error: string) => void;
}

export const createPoolsSlice: StateCreator<
  MinerStore,
  [["zustand/immer", never], ["zustand/devtools", never]],
  [],
  PoolsSlice
> = (set, get) => ({
  resourceByOwner: new Map(),
  byOwner: new Map(),
  setConfigurationMode: (service, value) => {
    const current = get().pools.configurationMode;
    if (current?.service === service && current.value === value) return;
    set((state) => {
      state.pools.configurationMode = { service, value };
    });
  },
  setPending: (pending) =>
    set((state) => {
      const previous = state.pools.resourceByOwner.get(ROOT_MODULE_KEY);
      // Keep a failure visible during retries, until a successful response clears it.
      if (pending && previous?.status !== "error") {
        state.pools.resourceByOwner.set(ROOT_MODULE_KEY, { status: "loading", updatedAtMs: previous?.updatedAtMs });
      } else if (!pending && previous?.status === "loading") {
        state.pools.resourceByOwner.set(
          ROOT_MODULE_KEY,
          previous.updatedAtMs === undefined
            ? { status: "idle" }
            : { status: "ready", updatedAtMs: previous.updatedAtMs },
        );
      }
    }),
  setError: (message) =>
    set((state) => {
      const previous = state.pools.resourceByOwner.get(ROOT_MODULE_KEY);
      state.pools.resourceByOwner.set(ROOT_MODULE_KEY, {
        status: "error",
        updatedAtMs: previous?.updatedAtMs,
        error: { code: "POOLS", message, retryable: true },
      });
    }),
  setPoolsInfo: (poolsInfo) =>
    set(
      (state) => {
        const resource: ResourceState = poolsInfo ? { status: "ready", updatedAtMs: Date.now() } : { status: "idle" };
        state.pools.resourceByOwner.set(ROOT_MODULE_KEY, resource);
        if (poolsInfo) state.pools.byOwner.set(ROOT_MODULE_KEY, poolsInfo);
        else state.pools.byOwner.delete(ROOT_MODULE_KEY);
      },
      false,
      "pools/setPoolsInfo",
    ),
});
