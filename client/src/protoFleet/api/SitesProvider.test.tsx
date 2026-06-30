import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code } from "@connectrpc/connect";

import { SiteSchema, type SiteWithCounts, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useSitesContext } from "@/protoFleet/api/SitesContext";
import { SitesProvider } from "@/protoFleet/api/SitesProvider";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const listSitesMock = vi.hoisted(() => vi.fn());
vi.mock("@/protoFleet/api/sites", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/protoFleet/api/sites")>();
  return {
    ...actual,
    useSites: () => ({ listSites: listSitesMock }),
  };
});

const hasPermissionMock = vi.hoisted(() => ({ current: (_key: string): boolean => true }));
vi.mock("@/protoFleet/store", () => ({
  useHasPermission: (key: string) => hasPermissionMock.current(key),
  useAuthErrors: () => ({ handleAuthErrors: vi.fn() }),
}));

const makeSite = (id: number, name = `Site ${id}`): SiteWithCounts =>
  create(SiteWithCountsSchema, { site: create(SiteSchema, { id: BigInt(id), name }) });

// Surfaces the context value as text so assertions can read provider state.
const Probe = () => {
  const ctx = useSitesContext();
  return (
    <div>
      <span data-testid="count">{ctx.sites === undefined ? "loading" : String(ctx.sites.length)}</span>
      <span data-testid="error">{ctx.sitesError ?? "none"}</span>
      <span data-testid="loaded">{String(ctx.sitesLoaded)}</span>
      <span data-testid="settled">{String(ctx.sitesSettled)}</span>
      <span data-testid="denied">{String(ctx.sitesPermissionDenied)}</span>
      <span data-testid="granted">{String(ctx.siteCatalogAccessGranted)}</span>
    </div>
  );
};

const renderProvider = () =>
  render(
    <SitesProvider>
      <Probe />
    </SitesProvider>,
  );

beforeEach(() => {
  hasPermissionMock.current = () => true;
  listSitesMock.mockReset();
  listSitesMock.mockImplementation(async ({ onSuccess }) => onSuccess?.([makeSite(1), makeSite(2)]));
  useFleetStore.setState((state) => {
    state.ui.sitesRevision = 0;
  });
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("SitesProvider", () => {
  it("fetches once and publishes the catalog to consumers", async () => {
    renderProvider();

    await waitFor(() => expect(screen.getByTestId("count").textContent).toBe("2"));
    expect(listSitesMock).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("loaded").textContent).toBe("true");
    expect(screen.getByTestId("settled").textContent).toBe("true");
    expect(screen.getByTestId("granted").textContent).toBe("true");
  });

  it("skips the fetch entirely for callers without site:read", async () => {
    hasPermissionMock.current = (key) => key !== "site:read";

    renderProvider();

    // No fetch issued; consumers see an empty, settled catalog rather than a
    // permanent loading skeleton.
    expect(listSitesMock).not.toHaveBeenCalled();
    expect(screen.getByTestId("count").textContent).toBe("0");
    expect(screen.getByTestId("settled").textContent).toBe("true");
    expect(screen.getByTestId("granted").textContent).toBe("false");
  });

  it("surfaces a transient error while keeping the catalog settled", async () => {
    listSitesMock.mockImplementation(async ({ onError }) => onError?.("boom", Code.Unavailable));

    renderProvider();

    await waitFor(() => expect(screen.getByTestId("error").textContent).toBe("boom"));
    expect(screen.getByTestId("settled").textContent).toBe("true");
    expect(screen.getByTestId("loaded").textContent).toBe("false");
    expect(screen.getByTestId("denied").textContent).toBe("false");
  });

  it("flags PermissionDenied so the redirect waterfall can react", async () => {
    listSitesMock.mockImplementation(async ({ onError }) => onError?.("denied", Code.PermissionDenied));

    renderProvider();

    await waitFor(() => expect(screen.getByTestId("denied").textContent).toBe("true"));
    expect(screen.getByTestId("granted").textContent).toBe("false");
  });
});
