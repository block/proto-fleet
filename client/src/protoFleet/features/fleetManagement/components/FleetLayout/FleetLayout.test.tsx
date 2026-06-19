import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code } from "@connectrpc/connect";

// Force MULTI_SITE_ENABLED=true at module-load time so FleetLayout's
// TAB_ORDER includes Sites + Buildings under test. CI default is false; the
// tests below pin behavior to the flag-on path explicitly.
vi.mock("@/protoFleet/constants/featureFlags", () => ({
  MULTI_SITE_ENABLED: true,
}));

import FleetLayout from "./FleetLayout";
import { SiteSchema, type SiteWithCounts, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { type ActiveSite } from "@/protoFleet/store/types/activeSite";

// Mock listSites at the hook level so the test stays focused on FleetLayout's
// redirect logic. The hook returns a callable that resolves with the
// provided sites via onSuccess — same shape as the real listSites contract.
const listSitesMock = vi.hoisted(() => vi.fn());
vi.mock("@/protoFleet/api/sites", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/protoFleet/api/sites")>();
  return {
    ...actual,
    useSites: () => ({
      listSites: listSitesMock,
      // The other useSites members are unused in FleetLayout but the type
      // shape requires them; stub as no-ops.
      createSite: vi.fn(),
      updateSite: vi.fn(),
      deleteSite: vi.fn(),
      assignDevicesToSite: vi.fn(),
    }),
  };
});

// Mock useActiveSite so each test can pin the SitePicker selection without
// driving the Zustand store. The hook returns { activeSite, setActiveSite }.
const activeSiteMock = vi.hoisted(() => ({ current: { kind: "all" } as ActiveSite }));
vi.mock("@/protoFleet/components/PageHeader/SitePicker", () => ({
  useActiveSite: () => ({
    activeSite: activeSiteMock.current,
    setActiveSite: vi.fn(),
  }),
}));

// Mock useHasPermission so site:read returns true by default — most tests
// pin the full-access path. Individual tests can override by setting
// `hasPermissionMock.current` before render.
const hasPermissionMock = vi.hoisted(() => ({ current: (_key: string): boolean => true }));
vi.mock("@/protoFleet/store", () => ({
  useHasPermission: (key: string) => hasPermissionMock.current(key),
  useAuthErrors: () => ({ handleAuthErrors: vi.fn() }),
  useUsername: () => "alice",
}));

// CompleteSetup renders inside FleetLayout's chrome but isn't under test
// here — stub it so we don't pull in onboarding's RPC/zustand surface area.
// The sentinel lets us assert the miner:read gate keeps it from mounting.
vi.mock("@/protoFleet/features/onboarding/components/CompleteSetup/CompleteSetup", () => ({
  default: () => <div data-testid="complete-setup-mock" />,
}));

const buildSite = (id: number, name = `Site ${id}`): SiteWithCounts =>
  create(SiteWithCountsSchema, {
    site: create(SiteSchema, { id: BigInt(id), name }),
    deviceCount: 0n,
    rackCount: 0n,
    buildingCount: 0n,
  });

const LocationProbe = () => {
  const location = useLocation();
  return <div data-testid="location-probe">{location.pathname}</div>;
};

const renderAt = (initialPath: string) =>
  render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route path="/fleet" element={<FleetLayout />}>
          <Route index element={null} />
          <Route path="sites" element={<div data-testid="tab-content-sites">sites</div>} />
          <Route path="buildings" element={<div data-testid="tab-content-buildings">buildings</div>} />
          <Route path="racks" element={<div data-testid="tab-content-racks">racks</div>} />
          <Route path="miners" element={<div data-testid="tab-content-miners">miners</div>} />
        </Route>
      </Routes>
      <LocationProbe />
    </MemoryRouter>,
  );

beforeEach(() => {
  // Default: listSites resolves with a non-empty set so the redirect
  // gate (waits for sites !== undefined) clears. Tests that need to
  // exercise the in-flight branch override before render.
  listSitesMock.mockImplementation(async ({ onSuccess }) => {
    onSuccess?.([buildSite(1), buildSite(2)]);
  });
  activeSiteMock.current = { kind: "all" };
  localStorage.clear();
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("FleetLayout redirect logic", () => {
  test("bare /fleet redirects to Sites tab when picker is All Sites and no lastTab is stored", async () => {
    renderAt("/fleet");
    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/sites"));
    expect(screen.getByTestId("tab-content-sites")).toBeInTheDocument();
  });

  test("bare /fleet redirects to the stored lastTab when one exists", async () => {
    localStorage.setItem("fleet:lastActiveTab", JSON.stringify("racks"));
    renderAt("/fleet");
    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/racks"));
  });

  test("bare /fleet falls back to Buildings when picker is single-site and lastTab is 'sites'", async () => {
    localStorage.setItem("fleet:lastActiveTab", JSON.stringify("sites"));
    activeSiteMock.current = { kind: "site", id: "1" };
    renderAt("/fleet");
    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/buildings"));
  });

  test("ignores a corrupted lastTab value and falls back to the default tab", async () => {
    // Older schema or manual tampering: lastTab is a string but not a
    // FleetTabId. Without the isFleetTabId guard the layout would navigate
    // to /fleet/<garbage>.
    localStorage.setItem("fleet:lastActiveTab", JSON.stringify("dashboard"));
    renderAt("/fleet");
    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/sites"));
  });

  test("Sites tab redirects to /sites/:id when SitePicker selects a single site", async () => {
    // Fleet Sites entry points resolve to that site's management detail page
    // when the picker is pinned, rather than bouncing to Buildings.
    activeSiteMock.current = { kind: "site", id: "1" };
    renderAt("/fleet/sites");
    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/sites/1"));
  });

  test("Sites tab stays visible under Unassigned picker selection", async () => {
    activeSiteMock.current = { kind: "unassigned" };
    renderAt("/fleet/sites");
    // No redirect — content for the Sites tab still renders.
    await waitFor(() => expect(screen.getByTestId("tab-content-sites")).toBeInTheDocument());
    expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/sites");
  });

  test("does not redirect away from a non-sites tab when picker hides Sites", async () => {
    activeSiteMock.current = { kind: "site", id: "1" };
    renderAt("/fleet/racks");
    // Operator is on a non-hidden tab; layout must leave them there.
    await waitFor(() => expect(screen.getByTestId("tab-content-racks")).toBeInTheDocument());
    expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/racks");
  });
});

describe("FleetLayout lastTab persistence", () => {
  test("writes the current tab to localStorage on navigation", async () => {
    renderAt("/fleet/buildings");
    await waitFor(() => expect(screen.getByTestId("tab-content-buildings")).toBeInTheDocument());
    await waitFor(() => {
      expect(localStorage.getItem("fleet:lastActiveTab")).toBe(JSON.stringify("buildings"));
    });
  });
});

describe("FleetLayout scoped-permission fallback", () => {
  test("falls back to /fleet/miners when listSites returns PermissionDenied", async () => {
    // useHasPermission("site:read") returns true (flat union across scopes),
    // but the org-scoped ListSites call is denied for site-scoped-only roles.
    // The layout must treat that as an access-blocked signal and land the
    // operator on the still-accessible Miners tab.
    listSitesMock.mockImplementation(async ({ onError }) => {
      onError?.("access denied", Code.PermissionDenied);
    });
    renderAt("/fleet");
    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/miners"));
  });

  test("ignores stored lastTab=racks when site access is blocked", async () => {
    // A persisted "racks" pick must not override the Miners safe path —
    // rack:read can be denied for the same role that lacks site:read,
    // and landing on /fleet/racks would just show another permission error.
    localStorage.setItem("fleet:lastActiveTab", JSON.stringify("racks"));
    hasPermissionMock.current = (key: string) => key !== "site:read";
    renderAt("/fleet");
    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/miners"));
  });
});

describe("FleetLayout CompleteSetup gate", () => {
  test("renders CompleteSetup when miner:read is held", async () => {
    renderAt("/fleet/miners");
    await waitFor(() => expect(screen.getByTestId("complete-setup-mock")).toBeInTheDocument());
  });

  test("hides CompleteSetup when miner:read is denied", async () => {
    // useAuthNeededMiners and usePoolNeededCount inside CompleteSetup call
    // ListMinerStateSnapshots (PermMinerRead). Roles without miner:read
    // shouldn't get permission-denied toasts just from opening a non-miner tab.
    hasPermissionMock.current = (key: string) => key !== "miner:read";
    renderAt("/fleet/racks");
    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/racks"));
    expect(screen.queryByTestId("complete-setup-mock")).not.toBeInTheDocument();
  });
});

describe("FleetLayout redirect gating on sites load", () => {
  test("defers the redirect until the initial sites load resolves", async () => {
    // Hold the listSites promise so sites === undefined for one frame.
    let resolveSites: ((sites: SiteWithCounts[]) => void) | null = null;
    listSitesMock.mockImplementation(async ({ onSuccess }) => {
      await new Promise<void>((resolve) => {
        resolveSites = (sites: SiteWithCounts[]) => {
          onSuccess?.(sites);
          resolve();
        };
      });
    });
    // Stale picker selection points at a now-deleted site; once sites land,
    // useActiveSite would normally reset to "all" — meanwhile, the layout's
    // redirect must NOT fire and bounce the operator off /fleet.
    activeSiteMock.current = { kind: "site", id: "999" };
    renderAt("/fleet");
    // Before sites resolve: still on /fleet (no redirect).
    expect(screen.getByTestId("location-probe").textContent).toBe("/fleet");

    await act(async () => {
      resolveSites?.([buildSite(1)]);
    });
    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).not.toBe("/fleet"));
  });
});
