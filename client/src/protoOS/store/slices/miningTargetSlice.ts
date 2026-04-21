import type { StateCreator } from "zustand";
import type { MinerStore } from "../useMinerStore";
import { HttpResponse, MiningTargetResponse, PerformanceMode } from "@/protoOS/api/generatedApi";

// =============================================================================
// Mining Target Slice Interface
// =============================================================================

export interface MiningTargetSlice {
  // State
  value?: number;
  default?: number;
  performanceMode?: PerformanceMode;
  bounds?: {
    min: number;
    max: number;
  };
  pending: boolean;
  error: string | null;

  // Actions
  setValue: (value: number) => void;
  setDefault: (defaultValue: number) => void;
  setPerformanceMode: (mode: PerformanceMode) => void;
  setBounds: (bounds: { min: number; max: number }) => void;
  setPending: (pending: boolean) => void;
  setError: (error: string | null) => void;
  setFromResponse: (response: HttpResponse<MiningTargetResponse>) => void;
  reset: () => void;
}

// =============================================================================
// Mining Target Slice Implementation
// =============================================================================

export const createMiningTargetSlice: StateCreator<MinerStore, [["zustand/immer", never]], [], MiningTargetSlice> = (
  set,
) => ({
  // Initial State
  value: undefined,
  default: undefined,
  performanceMode: undefined,
  bounds: undefined,
  pending: false,
  error: null,

  // Actions
  setValue: (value) =>
    set((state) => {
      state.miningTarget.value = value;
    }),

  setDefault: (defaultValue) =>
    set((state) => {
      state.miningTarget.default = defaultValue;
    }),

  setPerformanceMode: (mode) =>
    set((state) => {
      state.miningTarget.performanceMode = mode;
    }),

  setBounds: (bounds) =>
    set((state) => {
      state.miningTarget.bounds = bounds;
    }),

  setPending: (pending) =>
    set((state) => {
      state.miningTarget.pending = pending;
    }),

  setError: (error) =>
    set((state) => {
      state.miningTarget.error = error;
    }),

  setFromResponse: (response) =>
    set((state) => {
      state.miningTarget.value = response?.data.power_target_watts;
      state.miningTarget.default = response?.data.default_power_target_watts;
      state.miningTarget.performanceMode = response?.data.performance_mode;
      state.miningTarget.bounds = {
        min: response?.data.power_target_min_watts ?? 0,
        max: response?.data.power_target_max_watts ?? 0,
      };
      state.miningTarget.pending = false;
      state.miningTarget.error = null;
    }),

  reset: () =>
    set((state) => {
      state.miningTarget.value = undefined;
      state.miningTarget.default = undefined;
      state.miningTarget.performanceMode = undefined;
      state.miningTarget.bounds = undefined;
      state.miningTarget.pending = false;
      state.miningTarget.error = null;
    }),
});
