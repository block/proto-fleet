import type { StateCreator } from "zustand";

import {
  asEntityKey,
  type MinerError,
  type MinerStatusState,
  type OperationalStatus,
  ROOT_MODULE_KEY,
} from "../contract";
import type { MinerStore } from "../useMinerStore";

export interface MinerStatusSlice extends MinerStatusState {
  setErrors: (errors: MinerError[]) => void;
  setMiningStatus: (status: OperationalStatus | undefined) => void;
  setOnboarded: (onboarded: boolean | undefined) => void;
  setPasswordSet: (passwordSet: boolean | undefined) => void;
  setDefaultPasswordActive: (defaultPasswordActive: boolean | undefined) => void;
}

export const createMinerStatusSlice: StateCreator<
  MinerStore,
  [["zustand/immer", never], ["zustand/devtools", never]],
  [],
  MinerStatusSlice
> = (set) => ({
  operationalByOwner: new Map(),
  errorsByOwner: new Map(),
  lifecycle: { resource: { status: "idle" } },

  setErrors: (errors) =>
    set(
      (state) => {
        const byOwner = new Map<ReturnType<typeof asEntityKey>, MinerError[]>();
        for (const error of errors) {
          const key = asEntityKey(error.owner.id);
          const group = byOwner.get(key) ?? [];
          group.push(error);
          byOwner.set(key, group);
        }
        state.minerStatus.errorsByOwner = byOwner;
      },
      false,
      "minerStatus/setErrors",
    ),

  setMiningStatus: (status) =>
    set(
      (state) => {
        if (status) state.minerStatus.operationalByOwner.set(ROOT_MODULE_KEY, status);
        else state.minerStatus.operationalByOwner.delete(ROOT_MODULE_KEY);
      },
      false,
      "minerStatus/setMiningStatus",
    ),

  setOnboarded: (onboarded) =>
    set(
      (state) => {
        state.minerStatus.lifecycle.onboarded = onboarded;
        state.minerStatus.lifecycle.resource = { status: "ready", updatedAtMs: Date.now() };
      },
      false,
      "minerStatus/setOnboarded",
    ),

  setPasswordSet: (passwordSet) =>
    set(
      (state) => {
        state.minerStatus.lifecycle.passwordSet = passwordSet;
        state.minerStatus.lifecycle.resource = { status: "ready", updatedAtMs: Date.now() };
      },
      false,
      "minerStatus/setPasswordSet",
    ),

  setDefaultPasswordActive: (defaultPasswordActive) =>
    set(
      (state) => {
        state.minerStatus.lifecycle.defaultPasswordActive = defaultPasswordActive;
        state.minerStatus.lifecycle.resource = { status: "ready", updatedAtMs: Date.now() };
      },
      false,
      "minerStatus/setDefaultPasswordActive",
    ),
});
