import type { StateCreator } from "zustand";
import type { Measurement, MinerError } from "../types";
import type { MinerStore } from "../useMinerStore";
import type { MiningStatusMiningstatus } from "@/protoOS/api/generatedApi";

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
  errors: MinerError[];
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

  // System status
  onboarded: boolean | undefined;
  passwordSet: boolean | undefined;
  defaultPasswordActive: boolean | undefined;

  // Actions
  setErrors: (errors: MinerError[]) => void;
  setMiningStatus: (miningStatus: MiningStatusMiningstatus | undefined) => void;
  setOnboarded: (onboarded: boolean | undefined) => void;
  setPasswordSet: (passwordSet: boolean | undefined) => void;
  setDefaultPasswordActive: (defaultPasswordActive: boolean | undefined) => void;
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
    errors: [],
  },
  onboarded: undefined,
  passwordSet: undefined,
  defaultPasswordActive: undefined,

  // Actions
  setErrors: (errors) =>
    set(
      (state) => {
        state.minerStatus.errors = {
          errors: errors,
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
      },
      false,
      "minerStatus/setMiningStatus",
    ),

  setOnboarded: (onboarded) =>
    set(
      (state) => {
        state.minerStatus.onboarded = onboarded;
      },
      false,
      "minerStatus/setOnboarded",
    ),

  setPasswordSet: (passwordSet) =>
    set(
      (state) => {
        state.minerStatus.passwordSet = passwordSet;
      },
      false,
      "minerStatus/setPasswordSet",
    ),

  setDefaultPasswordActive: (defaultPasswordActive) =>
    set(
      (state) => {
        state.minerStatus.defaultPasswordActive = defaultPasswordActive;
      },
      false,
      "minerStatus/setDefaultPasswordActive",
    ),
});
