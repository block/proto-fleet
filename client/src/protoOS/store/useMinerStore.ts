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
        })),
        {
          name: "proto-ui-preferences", // Shared across protoOS and protoFleet
          partialize: (state) => ({
            ui: {
              duration: state.ui.duration,
              activeChartLines: state.ui.activeChartLines,
              theme: state.ui.theme,
              temperatureUnit: state.ui.temperatureUnit,
            },
          }),
          merge: (persistedState, currentState) => {
            // Ensure functions are preserved during rehydration
            return {
              ...currentState,
              ...(persistedState as any),
              ui: {
                ...currentState.ui,
                ...(persistedState as any)?.ui,
                // Preserve the functions from currentState
                setDuration: currentState.ui.setDuration,
                setActiveChartLines: currentState.ui.setActiveChartLines,
                toggleActiveChartLine: currentState.ui.toggleActiveChartLine,
                setTheme: currentState.ui.setTheme,
                setDeviceTheme: currentState.ui.setDeviceTheme,
                setTemperatureUnit: currentState.ui.setTemperatureUnit,
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
