import { MemoryRouter, Route, Routes, useNavigate } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code } from "@connectrpc/connect";

import {
  activeSiteFromScopablePath,
  activeSiteFromSegment,
  appEntryPath,
  isPathScopable,
  scopeCurrentOrDashboardPath,
  scopedPath,
  SiteScopeLayout,
  unscopedScopablePath,
  useRouteSiteScope,
} from "./siteScope";
import { SiteSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const resolveSiteBySlugMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/sites", async (importActual) => {
  const actual = await importActual<typeof import("@/protoFleet/api/sites")>();
  return {
    ...actual,
    useSites: () => ({ resolveSiteBySlug: resolveSiteBySlugMock }),
  };
});

const ScopeProbe = () => {
  const scope = useRouteSiteScope();
  const navigate = useNavigate();
  return (
    <>
      <div data-testid="scope-probe">{scope?.kind === "site" ? `${scope.slug}:${scope.id}` : scope?.kind}</div>
      <button type="button" onClick={() => navigate("/unassigned/dashboard")}>
        Unassigned
      </button>
      <button type="button" onClick={() => navigate("/north/dashboard")}>
        North
      </button>
    </>
  );
};

const renderSiteScopeRoute = (initialEntry: string) =>
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/" element={<div data-testid="home-page">Home</div>} />
        <Route path="/:siteScope" element={<SiteScopeLayout />}>
          <Route path="dashboard" element={<ScopeProbe />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );

beforeEach(() => {
  resolveSiteBySlugMock.mockReset();
  useFleetStore.setState((state) => {
    state.ui.activeSite = DEFAULT_ACTIVE_SITE;
  });
});

describe("siteScope routing helpers", () => {
  it("parses supported path scope segments", () => {
    const slugToId = new Map([["north-dc", "7"]]);
    expect(activeSiteFromSegment("north-dc", slugToId)).toEqual({ kind: "site", id: "7", slug: "north-dc" });
    expect(activeSiteFromSegment("unassigned")).toEqual({ kind: "unassigned" });
    expect(activeSiteFromSegment("fleet", slugToId)).toBeNull();
    expect(activeSiteFromSegment("north_dc", slugToId)).toBeNull();
    expect(activeSiteFromSegment("settings")).toBeNull();
    expect(activeSiteFromSegment("7", slugToId)).toBeNull();
  });

  it("strips path scope from scopable routes only", () => {
    expect(unscopedScopablePath("/fleet/miners")).toBe("/fleet/miners");
    expect(unscopedScopablePath("/north-dc/fleet/racks")).toBe("/fleet/racks");
    expect(unscopedScopablePath("/north-dc/dashboard")).toBe("/dashboard");
    expect(unscopedScopablePath("/north-dc/groups/team-a")).toBe("/north-dc/groups/team-a");
    expect(unscopedScopablePath("/unassigned/activity")).toBe("/activity");
    expect(unscopedScopablePath("/unassigned/fleet/buildings")).toBe("/fleet/buildings");
    expect(unscopedScopablePath("/settings/network")).toBe("/settings/network");
  });

  it("detects scopable paths", () => {
    expect(isPathScopable("/dashboard")).toBe(true);
    expect(isPathScopable("/fleet")).toBe(true);
    expect(isPathScopable("/north-dc/fleet/miners")).toBe(true);
    expect(isPathScopable("/north-dc/groups/team-a")).toBe(false);
    expect(isPathScopable("/energy")).toBe(true);
    expect(isPathScopable("/settings")).toBe(false);
  });

  it("derives the active site from scopable paths", () => {
    const slugToId = new Map([["north-dc", "7"]]);
    expect(activeSiteFromScopablePath("/dashboard")).toEqual({ kind: "all" });
    expect(activeSiteFromScopablePath("/fleet/miners")).toEqual({ kind: "all" });
    expect(activeSiteFromScopablePath("/north-dc/fleet/miners", slugToId)).toEqual({
      kind: "site",
      id: "7",
      slug: "north-dc",
    });
    expect(activeSiteFromScopablePath("/north-dc/activity", slugToId)).toEqual({
      kind: "site",
      id: "7",
      slug: "north-dc",
    });
    expect(activeSiteFromScopablePath("/unassigned/fleet/miners")).toEqual({ kind: "unassigned" });
    expect(activeSiteFromScopablePath("/settings/network")).toBeNull();
  });

  it("prefixes scopable paths while preserving search and hash", () => {
    expect(scopedPath("/fleet/miners?site=8#rows", { kind: "site", id: "7", slug: "north-dc" })).toBe(
      "/north-dc/fleet/miners?site=8#rows",
    );
    expect(scopedPath("/north-dc/fleet/miners?site=8", { kind: "all" })).toBe("/fleet/miners?site=8");
    expect(scopedPath("/fleet/racks", { kind: "unassigned" })).toBe("/unassigned/fleet/racks");
    expect(scopedPath("/dashboard", { kind: "site", id: "7", slug: "north-dc" })).toBe("/north-dc/dashboard");
    expect(scopedPath("/groups", { kind: "site", id: "7", slug: "north-dc" })).toBe("/north-dc/groups");
    expect(scopedPath("/groups/team-a", { kind: "site", id: "7", slug: "north-dc" })).toBe("/groups/team-a");
  });

  it("does not prefix non-scopable paths", () => {
    expect(scopedPath("/settings/team?tab=roles", { kind: "site", id: "7", slug: "north-dc" })).toBe(
      "/settings/team?tab=roles",
    );
  });

  it("maps app entry to the preferred Dashboard scope", () => {
    expect(appEntryPath({ kind: "all" })).toBe("/dashboard");
    expect(appEntryPath({ kind: "site", id: "7", slug: "north-dc" })).toBe("/north-dc/dashboard");
    expect(appEntryPath({ kind: "unassigned" })).toBe("/unassigned/dashboard");
  });

  it("uses the current scopable path for picker navigation and Dashboard landing elsewhere", () => {
    expect(
      scopeCurrentOrDashboardPath("/fleet/miners", "?model=s19", "#top", {
        kind: "site",
        id: "7",
        slug: "north-dc",
      }),
    ).toBe("/north-dc/fleet/miners?model=s19#top");
    expect(
      scopeCurrentOrDashboardPath("/activity", "?type=event", "#top", {
        kind: "site",
        id: "7",
        slug: "north-dc",
      }),
    ).toBe("/north-dc/activity?type=event#top");
    expect(
      scopeCurrentOrDashboardPath("/settings/team", "?tab=roles", "#top", {
        kind: "site",
        id: "7",
        slug: "north-dc",
      }),
    ).toBe("/north-dc/dashboard");
  });
});

describe("SiteScopeLayout", () => {
  it("resolves a slug route through ResolveSiteBySlug", async () => {
    resolveSiteBySlugMock.mockImplementation(({ onSuccess }) => {
      onSuccess(create(SiteSchema, { id: 7n, slug: "north" }));
    });

    renderSiteScopeRoute("/north/dashboard");

    await waitFor(() => expect(screen.getByTestId("scope-probe").textContent).toBe("north:7"));
    expect(resolveSiteBySlugMock).toHaveBeenCalledWith(expect.objectContaining({ slug: "north" }));
  });

  it("clears a stale stored site before redirecting an unknown slug home", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "7", slug: "missing" };
    });
    resolveSiteBySlugMock.mockImplementation(({ onError }) => {
      onError("not found", Code.NotFound);
    });

    renderSiteScopeRoute("/missing/dashboard");

    await waitFor(() => expect(screen.getByTestId("home-page")).toBeInTheDocument());
    expect(useFleetStore.getState().ui.activeSite).toEqual(DEFAULT_ACTIVE_SITE);
  });

  it("renders scoped routes without clobbering stored scope on transient slug resolution errors", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "7", slug: "north" };
    });
    resolveSiteBySlugMock.mockImplementation(({ onError }) => {
      onError("service unavailable", Code.Unavailable);
    });

    renderSiteScopeRoute("/north/dashboard");

    await waitFor(() => expect(resolveSiteBySlugMock).toHaveBeenCalledWith(expect.objectContaining({ slug: "north" })));
    expect(screen.getByTestId("scope-probe").textContent).toBe("");
    expect(screen.queryByTestId("home-page")).not.toBeInTheDocument();
    expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "7", slug: "north" });
  });

  it("retries transient slug resolution errors", async () => {
    vi.useFakeTimers();
    try {
      resolveSiteBySlugMock
        .mockImplementationOnce(({ onError }) => {
          onError("service unavailable", Code.Unavailable);
        })
        .mockImplementationOnce(({ onSuccess }) => {
          onSuccess(create(SiteSchema, { id: 7n, slug: "north" }));
        });

      renderSiteScopeRoute("/north/dashboard");

      expect(resolveSiteBySlugMock).toHaveBeenCalledTimes(1);
      expect(screen.getByTestId("scope-probe").textContent).toBe("");

      await act(async () => {
        await vi.advanceTimersByTimeAsync(2000);
      });

      expect(resolveSiteBySlugMock).toHaveBeenCalledTimes(2);
      expect(screen.getByTestId("scope-probe").textContent).toBe("north:7");
    } finally {
      vi.useRealTimers();
    }
  });

  it("clears a cached resolved slug when it later returns not found", async () => {
    resolveSiteBySlugMock
      .mockImplementationOnce(({ onSuccess }) => {
        onSuccess(create(SiteSchema, { id: 7n, slug: "north" }));
      })
      .mockImplementationOnce(({ onError }) => {
        onError("not found", Code.NotFound);
      });

    renderSiteScopeRoute("/north/dashboard");

    await waitFor(() => expect(screen.getByTestId("scope-probe").textContent).toBe("north:7"));
    fireEvent.click(screen.getByRole("button", { name: "Unassigned" }));
    await waitFor(() => expect(screen.getByTestId("scope-probe").textContent).toBe("unassigned"));
    fireEvent.click(screen.getByRole("button", { name: "North" }));

    await waitFor(() => expect(screen.getByTestId("home-page")).toBeInTheDocument());
  });
});
