import type { StateCreator } from "zustand";
import type { MinerStore } from "../useMinerStore";
import type { SystemInfoSysteminfo } from "@/protoOS/api/generatedApi";

// =============================================================================
// Device Type
// =============================================================================

/**
 * The kind of device this UI is connected to.
 *
 * A discriminant rather than a set of booleans: device-specific behavior keys
 * off one value, so adding a third device type doesn't turn every call site
 * into boolean algebra. Anything that isn't a container module is treated as a
 * rig, matching how the UI already defaulted.
 */
export type DeviceType = "rig" | "containerModule";

// INTERIM signal — no dedicated container/module-type flag exists on the miner
// yet (backend gap). Both Proto rigs and container modules report manufacturer
// "Proto"; the rig reports model "Rig" / product_name "Proto Rig" while the
// container module reports model "CU1" (left rail shows "Proto CU1"). Gate on
// the model prefix until a real type flag lands from proto/server, then swap
// this one line.
const CONTAINER_MODEL_PREFIX = "CU";

const toDeviceType = (model: string | undefined): DeviceType =>
  typeof model === "string" && model.toUpperCase().startsWith(CONTAINER_MODEL_PREFIX) ? "containerModule" : "rig";

// =============================================================================
// Slice Interface
// =============================================================================

export interface SystemInfoSlice extends SystemInfoSysteminfo {
  // Request state
  pending?: boolean;
  error?: string;

  /** Derived from `model` on every write; see `toDeviceType`. */
  deviceType?: DeviceType;

  // Actions
  setSystemInfo: (systemInfo: SystemInfoSysteminfo | undefined) => void;
  setError: (error: string | undefined) => void;
  setPending: (pending: boolean) => void;
  reset: () => void;
}

// =============================================================================
// Slice Creator
// =============================================================================

export const createSystemInfoSlice: StateCreator<MinerStore, [["zustand/immer", never]], [], SystemInfoSlice> = (
  set,
) => ({
  // Actions
  setSystemInfo: (systemInfo) =>
    set((state) => {
      if (systemInfo) {
        // Spread all fields from systemInfo into the root level
        Object.assign(state.systemInfo, systemInfo);
        // Derive from the merged state, not the payload: a partial update that
        // omits `model` must keep the device type already established.
        state.systemInfo.deviceType = toDeviceType(state.systemInfo.model);
      }
      // Note: When systemInfo is undefined, we don't clear fields -
      // they remain as is. To clear, pass an empty object {} instead.
    }),

  setError: (error) =>
    set((state) => {
      state.systemInfo.error = error;
      state.systemInfo.pending = false;
    }),

  setPending: (pending) =>
    set((state) => {
      state.systemInfo.pending = pending;
    }),

  // setSystemInfo(undefined) is a no-op by design (it only merges truthy
  // values), so a real clear must remove the flattened API fields. Delete every
  // data field (everything that isn't an action) so a miner switch fully clears
  // the previous device's system info.
  reset: () =>
    set((state) => {
      const slice = state.systemInfo as unknown as Record<string, unknown>;
      for (const key of Object.keys(slice)) {
        if (typeof slice[key] !== "function") {
          delete slice[key];
        }
      }
    }),
});
