import type { StateCreator } from "zustand";
import { ROOT_MODULE_KEY, type PoolInfo, type PoolsState, type ResourceState } from "../contract";
import type { MinerStore } from "../useMinerStore";

export interface PoolsSlice extends PoolsState {
  setPoolsInfo: (poolsInfo: PoolInfo[] | undefined) => void;
}

export const createPoolsSlice: StateCreator<
  MinerStore,
  [["zustand/immer", never], ["zustand/devtools", never]],
  [],
  PoolsSlice
> = (set) => ({
  resourceByOwner: new Map(),
  byOwner: new Map(),
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
