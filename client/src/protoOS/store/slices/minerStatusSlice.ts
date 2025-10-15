import type { StateCreator } from "zustand";
import type { Measurement } from "../types";
import type { MinerStore } from "../useMinerStore";
import type {
  ErrorListResponse,
  MiningStatusMiningstatus,
  Pool,
} from "@/protoOS/api/generatedApi";

// =============================================================================
// Types
// =============================================================================

export type MiningStatus =
  | "Uninitialized"
  | "PoweringOn"
  | "Mining"
  | "DegradedMining"
  | "PoweringOff"
  | "Stopped"
  | "NoPools"
  | "Error";

export interface ErrorsState {
  errors: ErrorListResponse | undefined;
  pending: boolean;
}

export interface PoolsInfoStatus {
  error: string;
  pending: boolean;
}

// =============================================================================
// Slice Interface
// =============================================================================

export interface MinerStatusSlice {
  // State - Flattened mining status fields
  miningStatus: MiningStatus | undefined;
  miningUptime: Measurement | undefined;
  rebootUptime: Measurement | undefined;
  hwErrors: number | undefined;
  message: string | undefined;

  // Other state
  errors: ErrorsState;
  poolsInfo: Pool[] | undefined;
  poolsInfoStatus: PoolsInfoStatus;

  // Actions
  setErrors: (errors: ErrorListResponse | undefined, pending: boolean) => void;
  setMiningStatus: (miningStatus: MiningStatusMiningstatus | undefined) => void;
  setPoolsInfo: (
    poolsInfo: Pool[] | undefined,
    error?: string,
    pending?: boolean,
  ) => void;
}

// =============================================================================
// Slice Creator
// =============================================================================

export const createMinerStatusSlice: StateCreator<
  MinerStore,
  [["zustand/immer", never], ["zustand/devtools", never]],
  [],
  MinerStatusSlice
> = (set) => ({
  // Initial State
  miningStatus: undefined,
  miningUptime: undefined,
  rebootUptime: undefined,
  hwErrors: undefined,
  message: undefined,
  errors: {
    errors: undefined,
    pending: false,
  },
  poolsInfo: undefined,
  poolsInfoStatus: {
    error: "",
    pending: false,
  },

  // Actions
  setErrors: (errors, pending) =>
    set(
      (state) => {
        state.minerStatus.errors = {
          errors: errors || [],
          pending: !!(pending && !errors),
        };
      },
      false,
      "minerStatus/setErrors",
    ),

  setMiningStatus: (apiMiningStatus) =>
    set(
      (state) => {
        if (!apiMiningStatus) {
          state.minerStatus.miningStatus = undefined;
          state.minerStatus.miningUptime = undefined;
          state.minerStatus.rebootUptime = undefined;
          state.minerStatus.hwErrors = undefined;
          state.minerStatus.message = undefined;
          return;
        }

        // Flatten and store only the fields we care about
        state.minerStatus.miningStatus = apiMiningStatus.status as MiningStatus;
        state.minerStatus.miningUptime = {
          value: apiMiningStatus.mining_uptime_s ?? null,
          units: undefined,
        };
        state.minerStatus.rebootUptime = {
          value: apiMiningStatus.reboot_uptime_s ?? null,
          units: undefined,
        };
        state.minerStatus.hwErrors = apiMiningStatus.hw_errors;
        state.minerStatus.message = apiMiningStatus.message;
      },
      false,
      "minerStatus/setMiningStatus",
    ),

  setPoolsInfo: (poolsInfo, error = "", pending = false) =>
    set(
      (state) => {
        state.minerStatus.poolsInfo = poolsInfo;
        state.minerStatus.poolsInfoStatus = {
          error,
          pending: pending && !poolsInfo,
        };
      },
      false,
      "minerStatus/setPoolsInfo",
    ),
});
