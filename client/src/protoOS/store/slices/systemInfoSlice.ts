import type { StateCreator } from "zustand";
import type { ResourceState, SystemInfoData, SystemInfoState } from "../contract";
import type { MinerStore } from "../useMinerStore";

export interface SystemInfoSlice extends SystemInfoState {
  setSystemInfo: (systemInfo: SystemInfoData | undefined) => void;
  setSystemTag: (systemTag: string) => void;
  setError: (error: string | undefined) => void;
  setPending: (pending: boolean) => void;
  reset: () => void;
}

const idleResource = (): ResourceState => ({ status: "idle" });

export const createSystemInfoSlice: StateCreator<MinerStore, [["zustand/immer", never]], [], SystemInfoSlice> = (
  set,
) => ({
  resource: idleResource(),
  data: undefined,

  setSystemInfo: (systemInfo) =>
    set((state) => {
      state.systemInfo.data = systemInfo;
      state.systemInfo.resource = systemInfo ? { status: "ready", updatedAtMs: Date.now() } : idleResource();
    }),

  setSystemTag: (systemTag) =>
    set((state) => {
      if (state.systemInfo.data) state.systemInfo.data.systemTag = systemTag;
    }),

  setError: (error) =>
    set((state) => {
      state.systemInfo.resource = error
        ? { status: "error", error: { code: "SYSTEM_INFO", message: error, retryable: true } }
        : idleResource();
    }),

  setPending: (pending) =>
    set((state) => {
      if (pending) state.systemInfo.resource = { status: "loading" };
    }),

  reset: () =>
    set((state) => {
      state.systemInfo.data = undefined;
      state.systemInfo.resource = idleResource();
    }),
});

export type { DeviceType } from "../contract";
