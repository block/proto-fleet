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
    act(() => result.current.setActiveSite({ kind: "site", id: "7", slug: "north" }));
    expect(result.current.activeSite).toEqual({ kind: "site", id: "7", slug: "north" });
    expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "north" });
  });

  it("reconciles a stale stored slug from knownSiteSlugById (rename in another session)", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "7", slug: "north-dc" };
    });
    renderHook(
      () =>
        useActiveSite({
          knownSiteIds: new Set(["7"]),
          knownSiteSlugById: new Map([["7", "south-dc"]]),
        }),
      { wrapper: routerWrapper(["/"]) },
    );
    // Same id, new slug → refresh the stored slug in place rather than letting
    // the dead slug clear the scope on the next ResolveSiteBySlug.
    await waitFor(() =>
      expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "south-dc" }),
    );
  });

  it("does not reconcile the stored slug while a route scope is active (route scope wins)", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "7", slug: "north-dc" };
    });
    // The URL is scoped to the same site with the old slug; the catalog map
    // reports a newer slug. Reconciliation must defer to the route-scope mirror
    // (which tracks the URL slug) so the two effects can't alternate forever.
    const { result } = renderHook(
      () =>
        useActiveSite({
          knownSiteIds: new Set(["7"]),
          knownSiteSlugById: new Map([["7", "south-dc"]]),
        }),
      { wrapper: routerWrapper(["/north-dc/activity"], { kind: "site", id: "7", slug: "north-dc" }) },
    );
    await waitFor(() =>
      expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "north-dc" }),
    );
    expect(result.current.activeSite).toEqual({ kind: "site", id: "7", slug: "north-dc" });
  });

  it("leaves the stored slug untouched when knownSiteSlugById matches", () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "7", slug: "north-dc" };
    });
    const { result } = renderHook(
      () =>
        useActiveSite({
          knownSiteIds: new Set(["7"]),
          knownSiteSlugById: new Map([["7", "north-dc"]]),
        }),
      { wrapper: routerWrapper(["/"]) },
    );
    expect(result.current.activeSite).toEqual({ kind: "site", id: "7", slug: "north-dc" });
  });

  it("falls back to { kind: 'all' } when the stored site id is not in the known set", () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "999", slug: "missing" };
    });
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set(["1", "2"]) }), {
      wrapper: routerWrapper(["/"]),
    });
    expect(result.current.activeSite).toEqual({ kind: "all" });
  });

  it("preserves a stored selection while known set is undefined (pre-fetch window)", () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "12", slug: "north" };
    });
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: undefined }), {
      wrapper: routerWrapper(["/"]),
    });
    // ListSites hasn't returned yet; do not clobber the selection.
    expect(result.current.activeSite).toEqual({ kind: "site", id: "12", slug: "north" });
  });

  it("falls back to { kind: 'all' } when the loaded known set is empty", () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "12", slug: "north" };
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
      wrapper: routerWrapper(["/north/activity"], { kind: "site", id: "7", slug: "north" }),
    });

    expect(result.current.activeSite).toEqual({ kind: "site", id: "7", slug: "north" });
    await waitFor(() =>
      expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "north" }),
    );
  });

  it("falls back to { kind: 'all' } when a route-scoped site is missing from an empty loaded set", () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "all" };
    });

    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set() }), {
      wrapper: routerWrapper(["/missing/activity"], { kind: "site", id: "999", slug: "missing" }),
    });

    expect(result.current.activeSite).toEqual({ kind: "all" });
  });

  it("heals a stale site URL by stripping the scope segment", async () => {
    renderHook(() => useActiveSite({ knownSiteIds: new Set(["1"]) }), {
      wrapper: routerWrapper(["/missing/activity"], { kind: "site", id: "999", slug: "missing" }),
    });

    // The route points at an unknown site, so the hook redirects to the
    // unscoped path; the picker/store reset to all-sites.
    await waitFor(() => expect(observedPath.current).toBe("/activity"));
    expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "all" });
  });
});
