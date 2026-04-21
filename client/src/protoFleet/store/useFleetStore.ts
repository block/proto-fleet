import { create } from "zustand";
import { devtools, persist, PersistStorage, StorageValue, subscribeWithSelector } from "zustand/middleware";
import { immer } from "zustand/middleware/immer";
import { type AuthSlice, createAuthSlice } from "./slices/authSlice";
import { type BatchSlice, createBatchSlice } from "./slices/batchSlice";
import { createOnboardingSlice, type OnboardingSlice } from "./slices/onboardingSlice";
import { createUISlice, type UISlice } from "./slices/uiSlice";
import {
  bulkRenameModes,
  normalizeBulkRenamePreferences,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/bulkRenameDefinitions";
import { isFleetDuration } from "@/shared/components/DurationSelector";

// =============================================================================
// Combined Store Interface
// =============================================================================

export interface FleetStore {
  auth: AuthSlice;
  batch: BatchSlice;
  ui: UISlice;
  onboarding: OnboardingSlice;
}

// =============================================================================
// Custom Multi-Key Storage
// =============================================================================

// Type for the partial state that we persist
type PersistedFleetState = {
  auth: Pick<AuthSlice, "sessionExpiry" | "isAuthenticated" | "username" | "role">;
  ui: Pick<
    UISlice,
    "theme" | "temperatureUnit" | "duration" | "bulkRenamePreferences" | "bulkWorkerNamePreferences" | "racksViewMode"
  >;
};

const createMultiKeyStorage = (): PersistStorage<PersistedFleetState> => {
  const AUTH_KEY = "proto-fleet-auth";
  const UI_KEY = "proto-ui-preferences";

  return {
    getItem: (): StorageValue<PersistedFleetState> | null => {
      // Load from both keys
      const authData = localStorage.getItem(AUTH_KEY);
      const uiData = localStorage.getItem(UI_KEY);

      const auth = authData ? JSON.parse(authData) : null;
      const ui = uiData ? JSON.parse(uiData) : null;

      if (!auth && !ui) return null;

      // Reconstruct Date objects from stored ISO strings
      if (auth?.state?.auth?.sessionExpiry) {
        auth.state.auth.sessionExpiry = new Date(auth.state.auth.sessionExpiry);
      }

      // Combine the data
      return {
        state: {
          ...(auth?.state || {}),
          ...(ui?.state || {}),
        },
        version: auth?.version || ui?.version || 0,
      } as StorageValue<PersistedFleetState>;
    },

    setItem: (_, value): void => {
      const state = value.state as PersistedFleetState;

      // Save auth data separately
      if (state.auth) {
        localStorage.setItem(
          AUTH_KEY,
          JSON.stringify({
            state: {
              auth: {
                sessionExpiry: state.auth.sessionExpiry,
                isAuthenticated: state.auth.isAuthenticated,
                username: state.auth.username,
                role: state.auth.role,
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
                theme: state.ui.theme,
                temperatureUnit: state.ui.temperatureUnit,
                duration: state.ui.duration,
                bulkRenamePreferences: state.ui.bulkRenamePreferences,
                bulkWorkerNamePreferences: state.ui.bulkWorkerNamePreferences,
                racksViewMode: state.ui.racksViewMode,
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

export const useFleetStore = create<FleetStore>()(
  devtools(
    subscribeWithSelector(
      persist(
        immer((set, get, api) => ({
          auth: createAuthSlice(set as any, get as any, api as any),
          batch: createBatchSlice(set as any, get as any, api as any),
          ui: createUISlice(set as any, get as any, api as any),
          onboarding: createOnboardingSlice(set as any, get as any, api as any),
        })),
        {
          name: "fleet-store",
          storage: createMultiKeyStorage(),
          partialize: (state) => ({
            auth: {
              sessionExpiry: state.auth.sessionExpiry,
              isAuthenticated: state.auth.isAuthenticated,
              username: state.auth.username,
              role: state.auth.role,
            },
            ui: {
              theme: state.ui.theme,
              temperatureUnit: state.ui.temperatureUnit,
              duration: state.ui.duration,
              bulkRenamePreferences: state.ui.bulkRenamePreferences,
              bulkWorkerNamePreferences: state.ui.bulkWorkerNamePreferences,
              racksViewMode: state.ui.racksViewMode,
            },
          }),
          merge: (persistedState, currentState) => {
            const persisted = persistedState as any;
            const hasPersistedSession = persisted?.auth?.isAuthenticated && persisted?.auth?.sessionExpiry;
            const persistedDuration = persisted?.ui?.duration;

            return {
              ...currentState,
              auth: {
                ...currentState.auth,
                sessionExpiry: persisted?.auth?.sessionExpiry ?? currentState.auth.sessionExpiry,
                isAuthenticated: persisted?.auth?.isAuthenticated ?? currentState.auth.isAuthenticated,
                username: persisted?.auth?.username ?? currentState.auth.username,
                role: persisted?.auth?.role ?? currentState.auth.role,
                // If we have persisted session, set loading to false
                authLoading: hasPersistedSession ? false : currentState.auth.authLoading,
              },
              ui: {
                ...currentState.ui,
                theme: persisted?.ui?.theme ?? currentState.ui.theme,
                temperatureUnit: persisted?.ui?.temperatureUnit ?? currentState.ui.temperatureUnit,
                duration: isFleetDuration(persistedDuration) ? persistedDuration : currentState.ui.duration,
                racksViewMode: persisted?.ui?.racksViewMode ?? currentState.ui.racksViewMode,
                bulkRenamePreferences: normalizeBulkRenamePreferences(
                  persisted?.ui?.bulkRenamePreferences ?? currentState.ui.bulkRenamePreferences,
                ),
                bulkWorkerNamePreferences: normalizeBulkRenamePreferences(
                  persisted?.ui?.bulkWorkerNamePreferences ?? currentState.ui.bulkWorkerNamePreferences,
                  bulkRenameModes.worker,
                ),
              },
            };
          },
        },
      ),
    ),
    {
      name: "fleet-store",
      serialize: {
        replacer: (_: string, value: unknown) => {
          // Handle BigInt (protobuf uses BigInt for 64-bit integers)
          if (typeof value === "bigint") {
            return value.toString();
          }
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
);
