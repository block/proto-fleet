import { type ReactNode, useLayoutEffect } from "react";

import AppLayout from "@/protoFleet/components/AppLayout";
import { type FleetStore, useFleetStore } from "@/protoFleet/store/useFleetStore";

// ----------------------------------------------------------------------------
// Shared Storybook decorator that frames a container page inside the real Fleet
// app chrome without a backend. AppLayout normally sits under the fleet `App`
// wrapper; mounting it directly avoids the health/auth gates while still using
// the real navigation and shell providers.
//
// The shell needs these permissions to show Fleet, Groups, Energy, and Activity.
// site:read is deliberately omitted so SitesProvider stays inert. The remaining
// shell probes use `/api-proxy/` and are routed to benign JSON by the fetch shim.
// Both the store and fetch are process-wide resources, so acquisition is
// reference-counted and the exact prior auth/storage/fetch state is restored
// after the last shell story unmounts. This keeps sibling stories isolated and
// also handles Storybook transitions with briefly overlapping mounts.
// ----------------------------------------------------------------------------
const FLEET_SHELL_PERMISSIONS = ["rack:read", "miner:read", "fleet:read", "curtailment:read", "activity:read"];
const FLEET_STORAGE_KEYS = ["proto-fleet-auth", "proto-ui-preferences", "proto-fleet-multi-site"] as const;

type AuthState = FleetStore["auth"];
type StorageSnapshot = Map<(typeof FLEET_STORAGE_KEYS)[number], string | null>;

let shellMountCount = 0;
let previousAuth: AuthState | null = null;
let previousFetch: typeof window.fetch | null = null;
let fleetFetchShim: typeof window.fetch | null = null;
let previousStorage: StorageSnapshot | null = null;

function snapshotStorage(): StorageSnapshot {
  return new Map(FLEET_STORAGE_KEYS.map((key) => [key, localStorage.getItem(key)]));
}

function restoreStorage(snapshot: StorageSnapshot): void {
  snapshot.forEach((value, key) => {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  });
}

function acquireFleetShell(): void {
  shellMountCount++;
  if (shellMountCount > 1) return;

  previousAuth = useFleetStore.getState().auth;
  previousFetch = window.fetch;
  previousStorage = snapshotStorage();

  const fetchToRestore = previousFetch;
  fleetFetchShim = (input, init) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
    if (!url.includes("/api-proxy/")) return fetchToRestore.call(window, input, init);
    return Promise.resolve(new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } }));
  };
  window.fetch = fleetFetchShim;

  useFleetStore.setState((state) => ({
    auth: {
      ...state.auth,
      permissions: FLEET_SHELL_PERMISSIONS,
      isAuthenticated: true,
      sessionExpiry: new Date(Date.now() + 365 * 24 * 60 * 60 * 1000),
      username: "demo",
      role: "admin",
      authLoading: false,
      temporaryPassword: null,
    },
  }));
}

function releaseFleetShell(): void {
  shellMountCount = Math.max(0, shellMountCount - 1);
  if (shellMountCount > 0) return;

  if (fleetFetchShim && previousFetch && window.fetch === fleetFetchShim) window.fetch = previousFetch;
  if (previousAuth) useFleetStore.setState({ auth: previousAuth });
  if (previousStorage) restoreStorage(previousStorage);

  previousAuth = null;
  previousFetch = null;
  fleetFetchShim = null;
  previousStorage = null;
}

/** Wraps children in the real Fleet AppLayout with isolated backend-free state. */
export function FleetShellStoryDecorator({ children }: { children: ReactNode }) {
  // Layout setup finishes before AppLayout's passive poll effects, preventing an
  // initial request from escaping while avoiding global mutation during render.
  useLayoutEffect(() => {
    acquireFleetShell();
    return releaseFleetShell;
  }, []);

  return <AppLayout hideShellHeader>{children}</AppLayout>;
}
