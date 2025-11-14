import type { StateCreator } from "zustand";
import type { MinerStore } from "../useMinerStore";
import type { Pool } from "@/protoOS/api/generatedApi";

// =============================================================================
// Slice Interface
// =============================================================================

export interface PoolsSlice {
  // State
  poolsInfo: Pool[] | undefined;

  // Actions
  setPoolsInfo: (poolsInfo: Pool[] | undefined) => void;
}

// =============================================================================
// Slice Creator
// =============================================================================

export const createPoolsSlice: StateCreator<
  MinerStore,
  [["zustand/immer", never], ["zustand/devtools", never]],
  [],
  PoolsSlice
> = (set) => ({
  // Initial State
  poolsInfo: undefined,

  // Actions
  setPoolsInfo: (poolsInfo) =>
    set(
      (state) => {
        state.pools.poolsInfo = poolsInfo;
      },
      false,
      "pools/setPoolsInfo",
    ),
});
