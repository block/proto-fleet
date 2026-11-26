import type { StateCreator } from "zustand";

import {
  asEntityKey,
  type MinerError,
  type MinerErrorObservations,
  type MinerStatusState,
  type OperationalStatus,
  ROOT_MODULE_KEY,
} from "../contract";
import type { MinerStore } from "../useMinerStore";
import { thermalFaultSeverity, visibleMinerErrors } from "../utils/visibleMinerErrors";

export interface MinerStatusSlice extends MinerStatusState {
  errorObservations?: { source: MinerErrorObservations; evaluatedAt: number };
  setErrors: (errors: MinerError[] | MinerErrorObservations, now?: number) => void;
  refreshThermalFaults: (now: number) => void;
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
> = (set, get) => ({
  operationalByOwner: new Map(),
  errorsByOwner: new Map(),
  lifecycle: { resource: { status: "idle" } },

  setErrors: (errors, now = Date.now()) =>
    set(
      (state) => {
        const source = Array.isArray(errors) ? { reports: errors, thermal: [] } : errors;
        const byOwner = new Map<ReturnType<typeof asEntityKey>, MinerError[]>();
        for (const error of visibleMinerErrors(source, now)) {
          const key = asEntityKey(error.owner.id);
          const group = byOwner.get(key) ?? [];
          group.push(error);
          byOwner.set(key, group);
        }
        state.minerStatus.errorsByOwner = byOwner;
        state.minerStatus.errorObservations = source.thermal.length ? { source, evaluatedAt: now } : undefined;
      },
      false,
      "minerStatus/setErrors",
    ),

  refreshThermalFaults: (now) => {
    const status = get().minerStatus;
    const observations = status.errorObservations;
    if (!observations) return;
    const { source, evaluatedAt } = observations;
    if (
      source.thermal.some(
        ({ samples }) => thermalFaultSeverity(samples, evaluatedAt) !== thermalFaultSeverity(samples, now),
      )
    ) {
      status.setErrors(source, now);
    }
  },

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
