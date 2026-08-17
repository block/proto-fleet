import type { StateCreator } from "zustand";

import { ROOT_MODULE_ID, type MiningTarget, type MiningTargetState, type PerformanceMode } from "../contract";
import type { MinerStore } from "../useMinerStore";

export interface MiningTargetSlice extends MiningTargetState {
  setValue: (value: number) => void;
  setDefault: (defaultValue: number) => void;
  setPerformanceMode: (mode: PerformanceMode) => void;
  setBounds: (bounds: { min: number; max: number }) => void;
  setPending: (pending: boolean) => void;
  setError: (error: string | null) => void;
  setFromResponse: (target: MiningTarget) => void;
  reset: () => void;
}

const emptyTarget = (): MiningTarget => ({
  moduleId: ROOT_MODULE_ID,
  resource: { status: "idle" },
});

export const createMiningTargetSlice: StateCreator<MinerStore, [["zustand/immer", never]], [], MiningTargetSlice> = (
  set,
) => ({
  byModule: new Map(),

  setValue: (value) =>
    set((state) => {
      const target = state.miningTarget.byModule.get(ROOT_MODULE_ID) ?? emptyTarget();
      target.valueWatts = value;
      state.miningTarget.byModule.set(ROOT_MODULE_ID, target);
    }),

  setDefault: (defaultValue) =>
    set((state) => {
      const target = state.miningTarget.byModule.get(ROOT_MODULE_ID) ?? emptyTarget();
      target.defaultWatts = defaultValue;
      state.miningTarget.byModule.set(ROOT_MODULE_ID, target);
    }),

  setPerformanceMode: (mode) =>
    set((state) => {
      const target = state.miningTarget.byModule.get(ROOT_MODULE_ID) ?? emptyTarget();
      target.performanceMode = mode;
      state.miningTarget.byModule.set(ROOT_MODULE_ID, target);
    }),

  setBounds: (bounds) =>
    set((state) => {
      const target = state.miningTarget.byModule.get(ROOT_MODULE_ID) ?? emptyTarget();
      target.boundsWatts = bounds;
      state.miningTarget.byModule.set(ROOT_MODULE_ID, target);
    }),

  setPending: (pending) =>
    set((state) => {
      const target = state.miningTarget.byModule.get(ROOT_MODULE_ID) ?? emptyTarget();
      target.resource = pending ? { status: "loading" } : { status: "ready", updatedAtMs: Date.now() };
      state.miningTarget.byModule.set(ROOT_MODULE_ID, target);
    }),

  setError: (error) =>
    set((state) => {
      const target = state.miningTarget.byModule.get(ROOT_MODULE_ID) ?? emptyTarget();
      target.resource = error
        ? { status: "error", error: { code: "MINING_TARGET", message: error, retryable: true } }
        : { status: "ready", updatedAtMs: Date.now() };
      state.miningTarget.byModule.set(ROOT_MODULE_ID, target);
    }),

  setFromResponse: (target) =>
    set((state) => {
      state.miningTarget.byModule.set(target.moduleId, target);
    }),

  reset: () =>
    set((state) => {
      state.miningTarget.byModule.clear();
    }),
});
