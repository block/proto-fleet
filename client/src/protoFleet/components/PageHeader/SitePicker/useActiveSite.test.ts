// useActiveSite is now a thin wrapper around the Zustand UI slice (see
// store/slices/uiSlice.ts + store/useFleetStore.ts). The hook itself only
// adds the "validate against knownSiteIds and reset to default if deleted"
// effect; persistence is now org-wide via Zustand persist middleware (no
// more per-username localStorage slots), matching the model already used
// for `duration`, theme, etc. The deleted per-username isolation test is
// intentionally gone — that contract no longer exists.
import { createElement, type ReactNode } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { useActiveSite } from "./useActiveSite";
import { SiteScopeProvider } from "@/protoFleet/routing/siteScope";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const resetActiveSite = () => {
  useFleetStore.setState((state) => {
    state.ui.activeSite = DEFAULT_ACTIVE_SITE;
  });
};

beforeEach(() => {
  resetActiveSite();
});

describe("useActiveSite", () => {
  it("returns the default { kind: 'all' } when the store is at its initial value", () => {
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set(["1", "2"]) }));
    expect(result.current.activeSite).toEqual({ kind: "all" });
  });

  it("persists writes through the Zustand store", () => {
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set(["7"]) }));
    act(() => result.current.setActiveSite({ kind: "site", id: "7" }));
    expect(result.current.activeSite).toEqual({ kind: "site", id: "7" });
    expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7" });
  });

  it("falls back to { kind: 'all' } when the stored site id is not in the known set", () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "999" };
    });
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set(["1", "2"]) }));
    expect(result.current.activeSite).toEqual({ kind: "all" });
  });

  it("preserves a stored selection while known set is undefined (pre-fetch window)", () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "12" };
    });
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: undefined }));
    // ListSites hasn't returned yet; do not clobber the selection.
    expect(result.current.activeSite).toEqual({ kind: "site", id: "12" });
  });

  it("falls back to { kind: 'all' } when the loaded known set is empty", () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "12" };
    });
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set() }));
    expect(result.current.activeSite).toEqual({ kind: "all" });
  });

  it("supports the unassigned selection variant", () => {
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set(["1"]) }));
    act(() => result.current.setActiveSite({ kind: "unassigned" }));
    expect(result.current.activeSite).toEqual({ kind: "unassigned" });
  });

  it("uses route scope as the source of truth and mirrors it to the store", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "all" };
    });
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(SiteScopeProvider, { value: { kind: "site", id: "7" }, children });

    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set(["7"]) }), { wrapper });

    expect(result.current.activeSite).toEqual({ kind: "site", id: "7" });
    await waitFor(() => expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7" }));
  });

  it("falls back to { kind: 'all' } when a route-scoped site is missing from an empty loaded set", () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "all" };
    });
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(SiteScopeProvider, { value: { kind: "site", id: "999" }, children });

    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set() }), { wrapper });

    expect(result.current.activeSite).toEqual({ kind: "all" });
  });
});
