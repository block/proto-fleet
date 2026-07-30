import { type ReactNode } from "react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code } from "@connectrpc/connect";

vi.mock("@/protoFleet/api/useSiteMapCsv", () => ({
  default: () => ({
    exportSiteMapCsv: vi.fn(),
    isExportingSiteMapCsv: false,
  }),
}));

const { completeSetupMock, refreshEntitiesMock } = vi.hoisted(() => ({
  completeSetupMock: vi.fn(),
  refreshEntitiesMock: vi.fn(),
}));

vi.mock("@/protoFleet/features/fleetManagement/components/FleetCreateFlow/context", () => ({
  useFleetCreateFlow: () => ({
    refreshEntities: refreshEntitiesMock,
  }),
}));

vi.mock("@/protoFleet/features/fleetManagement/components/SiteMapCsvImportModal", () => ({
  default: ({ open, onImported }: { open: boolean; onImported?: () => void }) =>
    open ? (
      <button type="button" data-testid="site-map-import-modal" onClick={onImported}>
        import
      </button>
    ) : null,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/FleetCreateFlow/FleetCreateFlowProvider", () => ({
  default: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

import FleetLayout from "./FleetLayout";
import { SiteSchema, type SiteWithCounts, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { SitesProvider } from "@/protoFleet/api/SitesProvider";
import { type ActiveSite } from "@/protoFleet/store/types/activeSite";

// Mock listSites at the hook level so the test stays focused on FleetLayout's
// redirect logic. The hook returns a callable that resolves with the
// provided sites via onSuccess — same shape as the real listSites contract.
const listSitesMock = vi.hoisted(() => vi.fn());
vi.mock("@/protoFleet/api/sites", () => {
  return {
    buildKnownSiteIds: (sites: SiteWithCounts[] | undefined): Set<string> | undefined => {
      if (!sites) return undefined;
      return new Set(sites.map((s) => (s.site?.id ?? 0n).toString()).filter((id) => id !== "0"));
    },
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

const installLocalStorageMock = () => {
  const storage = new Map<string, string>();
  const localStorageMock: Storage = {
    get length() {
      return storage.size;
    },
    clear: () => storage.clear(),
    getItem: (key) => storage.get(key) ?? null,
    key: (index) => Array.from(storage.keys())[index] ?? null,
    removeItem: (key) => {
      storage.delete(key);
    },
    setItem: (key, value) => {
      storage.set(key, value);
    },
  };

  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: localStorageMock,
  });
};

if (typeof globalThis.localStorage === "undefined") {
  installLocalStorageMock();
}

// CompleteSetup renders inside FleetLayout's chrome but isn't under test
// here — stub it so we don't pull in onboarding's RPC/zustand surface area.
// The sentinel lets us assert the miner:read gate keeps it from mounting.
vi.mock("@/protoFleet/features/onboarding/components/CompleteSetup/CompleteSetup", () => ({
  default: (props: { minersChangedAt?: number }) => {
    completeSetupMock(props);
    return <div data-testid="complete-setup-mock" data-miners-changed-at={props.minersChangedAt ?? 0} />;
  },
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
    // FleetLayout reads the site catalog from the shell-level SitesProvider,
    // which drives the (mocked) listSites + permission gating these tests
    // exercise. Wrapping here keeps the redirect/permission assertions intact.
    <SitesProvider>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route path="/fleet" element={<FleetLayout />}>
            <Route index element={null} />
            <Route path="sites" element={<div data-testid="tab-content-sites">sites</div>} />
            <Route path="buildings" element={<div data-testid="tab-content-buildings">buildings</div>} />
            <Route path="racks" element={<div data-testid="tab-content-racks">racks</div>} />
            <Route path="miners" element={<div data-testid="tab-content-miners">miners</div>} />
            <Route path="infrastructure" element={<div data-testid="tab-content-infrastructure">infrastructure</div>} />
          </Route>
        </Routes>
        <LocationProbe />
      </MemoryRouter>
    </SitesProvider>,
  );

beforeEach(() => {
  // Default: listSites resolves with a non-empty set so the redirect
  // gate (waits for sites !== undefined) clears. Tests that need to
  // exercise the in-flight branch override before render.
  listSitesMock.mockImplementation(async ({ onSuccess }) => {
    onSuccess?.([buildSite(1), buildSite(2)]);
  });
  activeSiteMock.current = { kind: "all" };
  hasPermissionMock.current = () => true;
  localStorage.clear();
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("FleetLayout redirect logic", () => {
  test("keeps the fleet content wrapper viewport-bound until desktop table scroll mode", async () => {
    renderAt("/fleet/miners");

    await waitFor(() => expect(screen.getByTestId("tab-content-miners")).toBeInTheDocument());

    expect(screen.getByTestId("fleet-layout")).toHaveClass("w-full", "min-w-0", "laptop:w-max", "laptop:min-w-full");
  });

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
    activeSiteMock.current = { kind: "site", id: "1", slug: "austin" };
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
    activeSiteMock.current = { kind: "site", id: "1", slug: "austin" };
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
    activeSiteMock.current = { kind: "site", id: "1", slug: "austin" };
    renderAt("/fleet/racks");
    // Operator is on a non-hidden tab; layout must leave them there.
    await waitFor(() => expect(screen.getByTestId("tab-content-racks")).toBeInTheDocument());
    expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/racks");
  });

  test("shows the Infrastructure tab to authorized users", async () => {
    renderAt("/fleet/racks");
    await waitFor(() => expect(screen.getByTestId("tab-content-racks")).toBeInTheDocument());
    expect(screen.getByTestId("fleet-tab-infrastructure")).toBeInTheDocument();
  });

  test("keeps Infrastructure deep links reachable and selected", async () => {
    renderAt("/fleet/infrastructure");
    await waitFor(() => expect(screen.getByTestId("tab-content-infrastructure")).toBeInTheDocument());
    expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/infrastructure");
    expect(screen.getByTestId("fleet-tab-infrastructure")).toBeInTheDocument();
  });

  test("redirects hidden tab deep links without mounting their content", async () => {
    hasPermissionMock.current = (key: string) => key !== "rack:read";

    renderAt("/fleet/racks");

    expect(screen.queryByTestId("tab-content-racks")).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/sites"));
    expect(screen.queryByTestId("tab-content-racks")).not.toBeInTheDocument();
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
  test("falls back to Racks when listSites returns PermissionDenied", async () => {
    // Keep the runtime PermissionDenied fallback for stale sessions or
    // server-side authz changes that can still deny the org-scoped ListSites
    // call after the client gate passes. Racks can still list without site
    // catalog metadata, so it remains the first visible Fleet tab.
    listSitesMock.mockImplementation(async ({ onError }) => {
      onError?.("access denied", Code.PermissionDenied);
    });
    renderAt("/fleet");
    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/racks"));
  });

  test("does not mount Infrastructure when site access is denied at runtime", async () => {
    hasPermissionMock.current = (key: string) => key === "site:read";
    listSitesMock.mockImplementation(async ({ onError }) => {
      onError?.("access denied", Code.PermissionDenied);
    });

    renderAt("/fleet/infrastructure");

    await waitFor(() => {
      expect(screen.getByText("You do not have permission to view Fleet sections.")).toBeInTheDocument();
    });
    expect(screen.queryByTestId("tab-content-infrastructure")).not.toBeInTheDocument();
  });

  test("keeps stored lastTab=racks when site access is blocked", async () => {
    localStorage.setItem("fleet:lastActiveTab", JSON.stringify("racks"));
    hasPermissionMock.current = (key: string) => key !== "site:read";
    renderAt("/fleet");
    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/racks"));
    expect(screen.getByTestId("tab-content-racks")).toBeInTheDocument();
  });

  test("does not mount a Fleet tab when no org-scoped Fleet read permissions are held", async () => {
    hasPermissionMock.current = () => false;
    renderAt("/fleet");
    await waitFor(() => {
      expect(screen.getByText("You do not have permission to view Fleet sections.")).toBeInTheDocument();
    });
    expect(screen.getByTestId("location-probe").textContent).toBe("/fleet");
    expect(screen.queryByTestId("tab-content-miners")).not.toBeInTheDocument();
    expect(screen.queryByTestId("tab-content-racks")).not.toBeInTheDocument();
  });

  test("shows permission denied on bare Fleet when only fleet read is held", async () => {
    hasPermissionMock.current = (key: string) => key === "fleet:read";

    renderAt("/fleet");

    await waitFor(() => {
      expect(screen.getByText("You do not have permission to view Fleet sections.")).toBeInTheDocument();
    });
    expect(screen.getByTestId("location-probe").textContent).toBe("/fleet");
    expect(screen.queryByTestId("fleet-tab-infrastructure")).not.toBeInTheDocument();
    expect(screen.queryByTestId("tab-content-infrastructure")).not.toBeInTheDocument();
  });

  test("shows permission denied on denied Fleet tabs when only fleet read is held", async () => {
    hasPermissionMock.current = (key: string) => key === "fleet:read";

    renderAt("/fleet/racks");

    await waitFor(() => {
      expect(screen.getByText("You do not have permission to view Fleet sections.")).toBeInTheDocument();
    });
    expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/racks");
    expect(screen.queryByTestId("fleet-tab-infrastructure")).not.toBeInTheDocument();
    expect(screen.queryByTestId("tab-content-racks")).not.toBeInTheDocument();
    expect(screen.queryByTestId("tab-content-infrastructure")).not.toBeInTheDocument();
  });

  test("does not mount Infrastructure deep links without site read", async () => {
    hasPermissionMock.current = (key: string) => key === "fleet:read";

    renderAt("/fleet/infrastructure");

    await waitFor(() => {
      expect(screen.getByText("You do not have permission to view Fleet sections.")).toBeInTheDocument();
    });
    expect(screen.queryByTestId("tab-content-infrastructure")).not.toBeInTheDocument();
  });

  test("mounts Infrastructure deep links for site-read roles", async () => {
    hasPermissionMock.current = (key: string) => key === "site:read";

    renderAt("/fleet/infrastructure");

    await waitFor(() => expect(screen.getByTestId("tab-content-infrastructure")).toBeInTheDocument());
    expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/infrastructure");
  });

  test("mounts Racks for rack-only roles without site metadata access", async () => {
    hasPermissionMock.current = (key: string) => key === "rack:read";

    renderAt("/fleet");

    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/racks"));
    expect(screen.getByTestId("tab-content-racks")).toBeInTheDocument();
  });

  test("does not mount Miners until its startup RPC permissions are held", async () => {
    hasPermissionMock.current = (key: string) => key === "miner:read";

    renderAt("/fleet");

    await waitFor(() => {
      expect(screen.getByText("You do not have permission to view Fleet sections.")).toBeInTheDocument();
    });
    expect(screen.queryByTestId("tab-content-miners")).not.toBeInTheDocument();
  });

  test("mounts Miners when miner and supporting read permissions are held", async () => {
    hasPermissionMock.current = (key: string) => key === "miner:read" || key === "rack:read" || key === "fleet:read";

    renderAt("/fleet/miners");

    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/miners"));
    expect(screen.getByTestId("tab-content-miners")).toBeInTheDocument();
  });

  test("mounts Miners for a Fleet+Miner role without rack:read", async () => {
    // Regression: a role with all Fleet + Miner permissions but no rack:read
    // (Sites/Buildings/Racks category) must still reach its miner list. The
    // miner list only needs miner:read + fleet:read; rack-backed filters
    // degrade rather than gate the tab.
    hasPermissionMock.current = (key: string) => key === "miner:read" || key === "fleet:read";

    renderAt("/fleet/miners");

    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).toBe("/fleet/miners"));
    expect(screen.getByTestId("tab-content-miners")).toBeInTheDocument();
    expect(screen.queryByText("You do not have permission to view Fleet sections.")).not.toBeInTheDocument();
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

describe("FleetLayout site map import refresh", () => {
  test("refreshes topology and miner data after a successful import", async () => {
    renderAt("/fleet/miners");

    await waitFor(() => expect(screen.getByTestId("tab-content-miners")).toBeInTheDocument());
    fireEvent.click(screen.getAllByText("Import site map")[0]);
    fireEvent.click(screen.getByTestId("site-map-import-modal"));

    expect(refreshEntitiesMock).toHaveBeenCalledTimes(1);
    await waitFor(() => {
      expect(Number(screen.getByTestId("complete-setup-mock").dataset.minersChangedAt)).toBeGreaterThan(0);
    });
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
    activeSiteMock.current = { kind: "site", id: "999", slug: "missing" };
    renderAt("/fleet");
    // Before sites resolve: still on /fleet (no redirect).
    expect(screen.getByTestId("location-probe").textContent).toBe("/fleet");

    await act(async () => {
      resolveSites?.([buildSite(1)]);
    });
    await waitFor(() => expect(screen.getByTestId("location-probe").textContent).not.toBe("/fleet"));
  });
});
