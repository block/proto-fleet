import { type ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { type ScopeSyncTarget, useSyncScopeToEntity } from "./useSyncScopeToEntity";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

// The detail routes this hook targets render outside SiteScopeLayout, so there
// is no SiteScopeContext provider — useRouteSiteScope() returns null and the
// store selection is the sole source of scope. A bare MemoryRouter reproduces
// that (satisfies useNavigate/useLocation without adding a route scope).
const wrapper = ({ children }: { children: ReactNode }) => <MemoryRouter>{children}</MemoryRouter>;

const renderSync = (target: ScopeSyncTarget | undefined) =>
  renderHook(({ t }: { t?: ScopeSyncTarget }) => useSyncScopeToEntity(t), {
    wrapper,
    initialProps: { t: target },
  });

const site = (id: string, slug: string): ScopeSyncTarget => ({ kind: "site", id, slug });

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

    renderSync(site("7", "dallas"));

    await waitFor(() =>
      expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "dallas" }),
    );
  });

  it("overwrites an 'unassigned' scope with the entity's own site", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "unassigned" };
    });

    renderSync(site("7", "dallas"));

    await waitFor(() =>
      expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "dallas" }),
    );
  });

  it("leaves an all-sites scope untouched for a scoped-site entity", async () => {
    renderSync(site("7", "dallas"));

    // Give the effect a chance to (not) run.
    await Promise.resolve();
    expect(useFleetStore.getState().ui.activeSite).toEqual(DEFAULT_ACTIVE_SITE);
  });

  it("is a no-op when the scope already matches the entity's site", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "7", slug: "dallas" };
    });

    renderSync(site("7", "dallas"));

    await Promise.resolve();
    expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "dallas" });
  });

  it("refreshes a stale slug when the id matches (post-rename reconciliation)", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "7", slug: "old-dallas" };
    });

    renderSync(site("7", "dallas"));

    await waitFor(() =>
      expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "dallas" }),
    );
  });

  it("moves a specific-site scope to unassigned for an unassigned entity", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "8", slug: "austin" };
    });

    renderSync({ kind: "unassigned" });

    await waitFor(() => expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "unassigned" }));
  });

  it("leaves an all-sites scope untouched for an unassigned entity", async () => {
    renderSync({ kind: "unassigned" });

    await Promise.resolve();
    expect(useFleetStore.getState().ui.activeSite).toEqual(DEFAULT_ACTIVE_SITE);
  });

  it("does nothing until the target is resolved", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "8", slug: "austin" };
    });

    // Entity/slug still loading → undefined target.
    const { rerender } = renderSync(undefined);
    await Promise.resolve();
    expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "8", slug: "austin" });

    // Target arrives → sync fires.
    rerender({ t: site("7", "dallas") });
    await waitFor(() =>
      expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "dallas" }),
    );
  });
});
