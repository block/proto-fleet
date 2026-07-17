import { type ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { useSyncScopeToEntity } from "./useSyncScopeToEntity";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

// The detail routes this hook targets render outside SiteScopeLayout, so there
// is no SiteScopeContext provider — useRouteSiteScope() returns null and the
// store selection is the sole source of scope. A bare MemoryRouter reproduces
// that (satisfies useNavigate/useLocation without adding a route scope).
const wrapper = ({ children }: { children: ReactNode }) => <MemoryRouter>{children}</MemoryRouter>;

const renderSync = (siteId: string | undefined, slug: string | undefined) =>
  renderHook(({ id, s }: { id?: string; s?: string }) => useSyncScopeToEntity(id, s), {
    wrapper,
    initialProps: { id: siteId, s: slug },
  });

describe("useSyncScopeToEntity", () => {
  beforeEach(() => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = DEFAULT_ACTIVE_SITE;
    });
  });

  it("overwrites a mismatched scoped site with the entity's own site", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "8", slug: "austin" };
    });

    renderSync("7", "dallas");

    await waitFor(() =>
      expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "dallas" }),
    );
  });

  it("overwrites an 'unassigned' scope with the entity's own site", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "unassigned" };
    });

    renderSync("7", "dallas");

    await waitFor(() =>
      expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "dallas" }),
    );
  });

  it("leaves an all-sites scope untouched", async () => {
    renderSync("7", "dallas");

    // Give the effect a chance to (not) run.
    await Promise.resolve();
    expect(useFleetStore.getState().ui.activeSite).toEqual(DEFAULT_ACTIVE_SITE);
  });

  it("is a no-op when the scope already matches the entity's site", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "7", slug: "dallas" };
    });

    renderSync("7", "dallas");

    await Promise.resolve();
    expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "dallas" });
  });

  it("refreshes a stale slug when the id matches (post-rename reconciliation)", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "7", slug: "old-dallas" };
    });

    renderSync("7", "dallas");

    await waitFor(() =>
      expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "dallas" }),
    );
  });

  it("does nothing until the entity's site id and slug are both resolved", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "8", slug: "austin" };
    });

    // id known but slug still resolving.
    const { rerender } = renderSync("7", undefined);
    await Promise.resolve();
    expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "8", slug: "austin" });

    // slug arrives → sync fires.
    rerender({ id: "7", s: "dallas" });
    await waitFor(() =>
      expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "dallas" }),
    );
  });
});
