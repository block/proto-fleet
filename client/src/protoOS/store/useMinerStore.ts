import { enableMapSet } from "immer";
import { create } from "zustand";
import { subscribeWithSelector } from "zustand/middleware";
import { devtools } from "zustand/middleware";
import { persist, PersistStorage, StorageValue } from "zustand/middleware";
import { immer } from "zustand/middleware/immer";
import { type AuthSlice, createAuthSlice } from "./slices/authSlice";
import { createHardwareSlice, type HardwareSlice } from "./slices/hardwareSlice";
import { createMinerStatusSlice, type MinerStatusSlice } from "./slices/minerStatusSlice";
import { createMiningTargetSlice, type MiningTargetSlice } from "./slices/miningTargetSlice";
import { createNetworkInfoSlice, type NetworkInfoSlice } from "./slices/networkInfoSlice";
import { createPoolsSlice, type PoolsSlice } from "./slices/poolsSlice";
import { createSystemInfoSlice, type SystemInfoSlice } from "./slices/systemInfoSlice";
import { createTelemetrySlice, type TelemetrySlice } from "./slices/telemetrySlice";
import { createUISlice, type UISlice } from "./slices/uiSlice";
import { isDuration } from "@/shared/components/DurationSelector";

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
  pools: PoolsSlice;
  systemInfo: SystemInfoSlice;
  networkInfo: NetworkInfoSlice;
  auth: AuthSlice;
  miningTarget: MiningTargetSlice;
}

// =============================================================================
// Custom Multi-Key Storage
// =============================================================================

// Type for the partial state that we persist
type PersistedState = {
  auth: Pick<AuthSlice, "authTokens">;
  ui: Pick<UISlice, "duration" | "activeChartLines" | "theme" | "temperatureUnit">;
};

const createMultiKeyStorage = (): PersistStorage<PersistedState> => {
  const AUTH_KEY = "proto-os-auth";
  const UI_KEY = "proto-ui-preferences";

  return {
    getItem: (): StorageValue<PersistedState> | null => {
      // Load from both keys
      const authData = localStorage.getItem(AUTH_KEY);
      const uiData = localStorage.getItem(UI_KEY);

      let auth = null;
      if (authData) {
        try {
          auth = JSON.parse(authData);
        } catch (e) {
          console.error("Failed to parse auth data from localStorage:", e);
          auth = null;
        }
      }
      let ui = null;
      if (uiData) {
        try {
          ui = JSON.parse(uiData);
        } catch (e) {
          console.error("Failed to parse UI data from localStorage:", e);
          ui = null;
        }
      }

      if (!auth && !ui) return null;

      // Combine the data
      return {
        state: {
          ...(auth?.state || {}),
          ...(ui?.state || {}),
        },
        version: auth?.version || ui?.version || 0,
      } as StorageValue<PersistedState>;
    },

    setItem: (_, value): void => {
      const state = value.state as PersistedState;

      // Save auth tokens separately
      if (state.auth) {
        localStorage.setItem(
          AUTH_KEY,
          JSON.stringify({
            state: {
              auth: {
                authTokens: state.auth.authTokens,
              },
            },
            version: value.version,
          }),
        );
      }

      // Save UI preferences separately
      if (state.ui) {
        localStorage.setItem(
          UI_KEY,
          JSON.stringify({
            state: {
              ui: {
                duration: state.ui.duration,
                activeChartLines: state.ui.activeChartLines,
                theme: state.ui.theme,
                temperatureUnit: state.ui.temperatureUnit,
              },
            },
            version: value.version,
          }),
        );
      }
    },

    removeItem: (): void => {
      localStorage.removeItem(AUTH_KEY);
      localStorage.removeItem(UI_KEY);
    },
  };
};

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
          pools: createPoolsSlice(set, get, api),
          systemInfo: createSystemInfoSlice(set, get, api),
          networkInfo: createNetworkInfoSlice(set, get, api),
          auth: createAuthSlice(set, get, api),
          miningTarget: createMiningTargetSlice(set, get, api),
        })),
        {
          name: "miner-store",
          storage: createMultiKeyStorage(),
          partialize: (state) => ({
            auth: {
              authTokens: state.auth.authTokens,
            },
            ui: {
              duration: state.ui.duration,
              activeChartLines: state.ui.activeChartLines,
              theme: state.ui.theme,
              temperatureUnit: state.ui.temperatureUnit,
            },
          }),
          merge: (persistedState, currentState) => {
            const persisted = persistedState as any;
            const persistedDuration = persisted?.ui?.duration;

            return {
              ...currentState,
              auth: {
                ...currentState.auth,
                authTokens: persisted?.auth?.authTokens ?? currentState.auth.authTokens,
              },
              ui: {
                ...currentState.ui,
                duration: isDuration(persistedDuration) ? persistedDuration : currentState.ui.duration,
                activeChartLines: persisted?.ui?.activeChartLines ?? currentState.ui.activeChartLines,
                theme: persisted?.ui?.theme ?? currentState.ui.theme,
                temperatureUnit: persisted?.ui?.temperatureUnit ?? currentState.ui.temperatureUnit,
              },
            };
          },
        },
      ),
      {
        name: "miner-store",
        serialize: {
          replacer: (_: string, value: unknown) => {
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
      } as Parameters<typeof devtools>[1],
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
