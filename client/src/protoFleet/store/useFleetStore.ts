import { create } from "zustand";
import { devtools, persist, PersistStorage, StorageValue, subscribeWithSelector } from "zustand/middleware";
import { immer } from "zustand/middleware/immer";
import { type AuthSlice, createAuthSlice } from "./slices/authSlice";
import { type BatchSlice, createBatchSlice } from "./slices/batchSlice";
import { createOnboardingSlice, type OnboardingSlice } from "./slices/onboardingSlice";
import { createUISlice, type UISlice } from "./slices/uiSlice";
import { isActiveSite } from "./types/activeSite";
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

const ORG_PERMISSIONS_SCOPE = "org" as const;

type PersistedAuthState = Pick<AuthSlice, "sessionExpiry" | "isAuthenticated" | "username" | "role" | "permissions"> & {
  // Guards against rehydrating old sessions where permissions meant a flat
  // "has this anywhere" projection. Current permissions are org/default scope.
  permissionsScope?: typeof ORG_PERMISSIONS_SCOPE;
};

// Type for the partial state that we persist
type PersistedFleetState = {
  auth: PersistedAuthState;
  ui: Pick<
    UISlice,
    | "theme"
    | "temperatureUnit"
    | "duration"
    | "bulkRenamePreferences"
    | "bulkWorkerNamePreferences"
    | "racksViewMode"
    | "activeSite"
  >;
};

const createMultiKeyStorage = (): PersistStorage<PersistedFleetState> => {
  const AUTH_KEY = "proto-fleet-auth";
  const UI_KEY = "proto-ui-preferences";
  // ActiveSite lives in its own protoFleet-scoped key so that protoOS's
  // useMinerStore (which writes to UI_KEY without an `activeSite` field in
  // its partialize) doesn't strip it from the shared UI blob whenever the
  // user touches both apps from the same origin.
  const MULTI_SITE_KEY = "proto-fleet-multi-site";

  return {
    getItem: (): StorageValue<PersistedFleetState> | null => {
      // Load from all three keys
      const authData = localStorage.getItem(AUTH_KEY);
      const uiData = localStorage.getItem(UI_KEY);
      const multiSiteData = localStorage.getItem(MULTI_SITE_KEY);

      const auth = authData ? JSON.parse(authData) : null;
      const ui = uiData ? JSON.parse(uiData) : null;
      const multiSite = multiSiteData ? JSON.parse(multiSiteData) : null;

      if (!auth && !ui && !multiSite) return null;

      // Reconstruct Date objects from stored ISO strings
      if (auth?.state?.auth?.sessionExpiry) {
        auth.state.auth.sessionExpiry = new Date(auth.state.auth.sessionExpiry);
      }

      // Combine the data. MultiSite's ui fields layer on top of the shared
      // UI blob so the fleet-scoped activeSite survives even when protoOS
      // overwrites UI_KEY without it.
      const baseUi = ui?.state?.ui ?? {};
      const multiSiteUi = multiSite?.state?.ui ?? {};
      return {
        state: {
          ...(auth?.state || {}),
          ui: { ...baseUi, ...multiSiteUi },
        },
        version: auth?.version || ui?.version || multiSite?.version || 0,
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
                permissions: state.auth.permissions,
                permissionsScope: ORG_PERMISSIONS_SCOPE,
              },
            },
            version: value.version,
          }),
        );
      }

      // Save shared UI preferences (collides cleanly with protoOS partialize).
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

      // Save protoFleet-only UI state to its own key so protoOS can't
      // strip it from the shared blob.
      if (state.ui) {
        localStorage.setItem(
          MULTI_SITE_KEY,
          JSON.stringify({
            state: {
              ui: {
                activeSite: state.ui.activeSite,
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
      localStorage.removeItem(MULTI_SITE_KEY);
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
              permissions: state.auth.permissions,
              permissionsScope: ORG_PERMISSIONS_SCOPE,
            },
            ui: {
              theme: state.ui.theme,
              temperatureUnit: state.ui.temperatureUnit,
              duration: state.ui.duration,
              bulkRenamePreferences: state.ui.bulkRenamePreferences,
              bulkWorkerNamePreferences: state.ui.bulkWorkerNamePreferences,
              racksViewMode: state.ui.racksViewMode,
              activeSite: state.ui.activeSite,
            },
          }),
          merge: (persistedState, currentState) => {
            const persisted = persistedState as any;
            const hasPersistedSession = persisted?.auth?.isAuthenticated && persisted?.auth?.sessionExpiry;
            // Pre-org-default localStorage either didn't carry a permissions
            // array or carried the old flat "has this anywhere" projection.
            // Drop those sessions so the next Authenticate repopulates
            // UserInfo.permissions with the current org/default projection.
            const hasPersistedPermissions = Array.isArray(persisted?.auth?.permissions);
            const hasPersistedOrgScopePermissions = persisted?.auth?.permissionsScope === ORG_PERMISSIONS_SCOPE;
            const sessionIsStalePreOrgDefault =
              hasPersistedSession && (!hasPersistedPermissions || !hasPersistedOrgScopePermissions);
            const persistedDuration = persisted?.ui?.duration;

            return {
              ...currentState,
              auth: {
                ...currentState.auth,
                sessionExpiry: sessionIsStalePreOrgDefault
                  ? currentState.auth.sessionExpiry
                  : (persisted?.auth?.sessionExpiry ?? currentState.auth.sessionExpiry),
                isAuthenticated: sessionIsStalePreOrgDefault
                  ? false
                  : (persisted?.auth?.isAuthenticated ?? currentState.auth.isAuthenticated),
                username: sessionIsStalePreOrgDefault
                  ? currentState.auth.username
                  : (persisted?.auth?.username ?? currentState.auth.username),
                role: sessionIsStalePreOrgDefault
                  ? currentState.auth.role
                  : (persisted?.auth?.role ?? currentState.auth.role),
                permissions: sessionIsStalePreOrgDefault
                  ? currentState.auth.permissions
                  : hasPersistedPermissions
                    ? persisted.auth.permissions
                    : currentState.auth.permissions,
                // If we have persisted session, set loading to false.
                // Stale pre-org-default sessions also stop loading so the
                // login redirect path engages immediately.
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
                activeSite: isActiveSite(persisted?.ui?.activeSite)
                  ? persisted.ui.activeSite
                  : currentState.ui.activeSite,
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
