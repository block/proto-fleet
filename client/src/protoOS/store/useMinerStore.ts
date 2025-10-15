import { enableMapSet } from "immer";
import { create } from "zustand";
import { subscribeWithSelector } from "zustand/middleware";
import { devtools } from "zustand/middleware";
import { persist } from "zustand/middleware";
import { immer } from "zustand/middleware/immer";
import {
  createHardwareSlice,
  type HardwareSlice,
} from "./slices/hardwareSlice";
import {
  createMinerStatusSlice,
  type MinerStatusSlice,
} from "./slices/minerStatusSlice";
import {
  createSystemInfoSlice,
  type SystemInfoSlice,
} from "./slices/systemInfoSlice";
import {
  createTelemetrySlice,
  type TelemetrySlice,
} from "./slices/telemetrySlice";
import { createUISlice, type UISlice } from "./slices/uiSlice";

// Enable Map/Set support for Immer
enableMapSet();

// =============================================================================
// Combined Store Interface
// =============================================================================

export interface MinerStore {
  hardware: HardwareSlice;
  telemetry: TelemetrySlice;
  ui: UISlice;
  minerStatus: MinerStatusSlice;
  systemInfo: SystemInfoSlice;
}

// =============================================================================
// Store Implementation
// =============================================================================

const useMinerStore = create<MinerStore>()(
  subscribeWithSelector(
    devtools(
      persist(
        immer((set, get, api) => ({
          hardware: createHardwareSlice(set, get, api),
          telemetry: createTelemetrySlice(set, get, api),
          ui: createUISlice(set, get, api),
          minerStatus: createMinerStatusSlice(set, get, api),
          systemInfo: createSystemInfoSlice(set, get, api),
        })),
        {
          name: "proto-ui-preferences", // Shared across protoOS and protoFleet
          partialize: (state) => ({
            ui: {
              duration: state.ui.duration,
              activeChartLines: state.ui.activeChartLines,
              theme: state.ui.theme,
              temperatureUnit: state.ui.temperatureUnit,
              // Note: deviceTheme is intentionally excluded from persistence
              // as it should be detected from the OS on each load
            },
          }),
          merge: (persistedState, currentState) => {
            const persisted = persistedState as any;

            // Ensure functions are preserved during rehydration
            return {
              ...currentState,
              ui: {
                ...currentState.ui,
                // Apply persisted UI state values, preserving functions
                duration: persisted?.ui?.duration ?? currentState.ui.duration,
                activeChartLines:
                  persisted?.ui?.activeChartLines ??
                  currentState.ui.activeChartLines,
                theme: persisted?.ui?.theme ?? currentState.ui.theme,
                temperatureUnit:
                  persisted?.ui?.temperatureUnit ??
                  currentState.ui.temperatureUnit,
                // deviceTheme is not persisted and should remain as currentState value (undefined initially)
                // It will be detected from OS by useApplyTheme on mount
                deviceTheme: currentState.ui.deviceTheme,
                // Preserve the functions from currentState
                setDuration: currentState.ui.setDuration,
                setActiveChartLines: currentState.ui.setActiveChartLines,
                toggleActiveChartLine: currentState.ui.toggleActiveChartLine,
                setTheme: currentState.ui.setTheme,
                setDeviceTheme: currentState.ui.setDeviceTheme,
                setTemperatureUnit: currentState.ui.setTemperatureUnit,
                showWakeDialog: currentState.ui.showWakeDialog,
                hideWakeDialog: currentState.ui.hideWakeDialog,
              },
            };
          },
        },
      ),
      {
        name: "miner-store",
        serialize: {
          replacer: (_: string, value: any) => {
            // Handle Maps
            if (value instanceof Map) {
              return Object.fromEntries(value);
            }
            // Handle functions (don't serialize them, just show their names)
            if (typeof value === "function") {
              return `[Function: ${value.name || "anonymous"}]`;
            }
            return value;
          },
        },
      },
    ),
  ),
);

export default useMinerStore;

// =============================================================================
// Store Subscriptions
// =============================================================================

// Clear time series data when duration changes (preserve latest values)
useMinerStore.subscribe(
  (state) => state.ui.duration,
  (duration, prevDuration) => {
    if (duration !== prevDuration) {
      // Clear only time series data, preserve latest polling data
      useMinerStore.getState().telemetry.clearTimeSeriesData();
    }
  },
);
