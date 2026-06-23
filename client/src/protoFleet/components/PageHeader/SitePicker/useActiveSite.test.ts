// useActiveSite is now a thin wrapper around the Zustand UI slice (see
// store/slices/uiSlice.ts + store/useFleetStore.ts). The hook itself only
// adds the "validate against knownSiteIds and reset to default if deleted"
// effect plus a stale-route-scope URL heal; persistence is now org-wide via
// Zustand persist middleware (no more per-username localStorage slots),
// matching the model already used for `duration`, theme, etc. The deleted
// per-username isolation test is intentionally gone — that contract no longer
// exists.
//
// The hook reads useLocation/useNavigate, so every render is wrapped in a
// MemoryRouter; routerWrapper composes that with an optional SiteScopeProvider
// to simulate a path scope.
import { createElement, type ReactNode, useEffect } from "react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { useActiveSite } from "./useActiveSite";
import { SiteScopeProvider } from "@/protoFleet/routing/siteScope";
import { type ActiveSite, DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const resetActiveSite = () => {
  useFleetStore.setState((state) => {
    state.ui.activeSite = DEFAULT_ACTIVE_SITE;
  });
};

const observedPath = { current: "" };
const LocationProbe = () => {
  const { pathname } = useLocation();
  useEffect(() => {
    observedPath.current = pathname;
  }, [pathname]);
  return null;
};

// Wrap the hook in a MemoryRouter (required by useLocation/useNavigate) and,
// optionally, a SiteScopeProvider simulating the active path scope.
const routerWrapper =
  (initialEntries: string[], scope?: ActiveSite) =>
  ({ children }: { children: ReactNode }) =>
    createElement(
      MemoryRouter,
      { initialEntries },
      createElement(LocationProbe),
      scope ? createElement(SiteScopeProvider, { value: scope, children }) : children,
    );

beforeEach(() => {
  resetActiveSite();
  observedPath.current = "";
});

describe("useActiveSite", () => {
  it("returns the default { kind: 'all' } when the store is at its initial value", () => {
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set(["1", "2"]) }), {
      wrapper: routerWrapper(["/"]),
    });
    expect(result.current.activeSite).toEqual({ kind: "all" });
  });

  it("persists writes through the Zustand store", () => {
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set(["7"]) }), {
      wrapper: routerWrapper(["/"]),
    });
    act(() => result.current.setActiveSite({ kind: "site", id: "7" }));
    expect(result.current.activeSite).toEqual({ kind: "site", id: "7" });
    expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7" });
  });

  it("falls back to { kind: 'all' } when the stored site id is not in the known set", () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "999" };
    });
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set(["1", "2"]) }), {
      wrapper: routerWrapper(["/"]),
    });
    expect(result.current.activeSite).toEqual({ kind: "all" });
  });

  it("preserves a stored selection while known set is undefined (pre-fetch window)", () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "12" };
    });
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: undefined }), {
      wrapper: routerWrapper(["/"]),
    });
    // ListSites hasn't returned yet; do not clobber the selection.
    expect(result.current.activeSite).toEqual({ kind: "site", id: "12" });
  });

  it("falls back to { kind: 'all' } when the loaded known set is empty", () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "12" };
    });
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set() }), {
      wrapper: routerWrapper(["/"]),
    });
    expect(result.current.activeSite).toEqual({ kind: "all" });
  });

  it("supports the unassigned selection variant", () => {
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set(["1"]) }), {
      wrapper: routerWrapper(["/"]),
    });
    act(() => result.current.setActiveSite({ kind: "unassigned" }));
    expect(result.current.activeSite).toEqual({ kind: "unassigned" });
  });

  it("uses route scope as the source of truth and mirrors it to the store", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "all" };
    });

    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set(["7"]) }), {
      wrapper: routerWrapper(["/7/activity"], { kind: "site", id: "7" }),
    });

    expect(result.current.activeSite).toEqual({ kind: "site", id: "7" });
    await waitFor(() => expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7" }));
  });

  it("falls back to { kind: 'all' } when a route-scoped site is missing from an empty loaded set", () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "all" };
    });

    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set() }), {
      wrapper: routerWrapper(["/999/activity"], { kind: "site", id: "999" }),
    });

    expect(result.current.activeSite).toEqual({ kind: "all" });
  });

  it("heals a stale site URL by stripping the scope segment", async () => {
    renderHook(() => useActiveSite({ knownSiteIds: new Set(["1"]) }), {
      wrapper: routerWrapper(["/999/activity"], { kind: "site", id: "999" }),
    });

    // The route points at an unknown site, so the hook redirects to the
    // unscoped path; the picker/store reset to all-sites.
    await waitFor(() => expect(observedPath.current).toBe("/activity"));
    expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "all" });
  });
});
