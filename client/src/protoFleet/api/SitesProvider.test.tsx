import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code } from "@connectrpc/connect";

import { SiteSchema, type SiteWithCounts, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useSitesContext, useSitesPolling } from "@/protoFleet/api/SitesContext";
import { SitesProvider } from "@/protoFleet/api/SitesProvider";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
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
      <button onClick={ctx.refetchSites}>refetch</button>
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

  it("flags PermissionDenied and clears the catalog so the picker can't show stale sites", async () => {
    // First load succeeds, then a later fetch is denied (mid-session authz change).
    listSitesMock.mockImplementationOnce(async ({ onSuccess }) => onSuccess?.([makeSite(1)]));
    renderProvider();
    await waitFor(() => expect(screen.getByTestId("count").textContent).toBe("1"));

    listSitesMock.mockImplementationOnce(async ({ onError }) => onError?.("denied", Code.PermissionDenied));
    fireEvent.click(screen.getByText("refetch"));

    await waitFor(() => expect(screen.getByTestId("denied").textContent).toBe("true"));
    // Last-good list is dropped (not preserved) on PermissionDenied.
    expect(screen.getByTestId("count").textContent).toBe("0");
    // sitesLoaded stays true (ever-loaded), but siteCatalogAccessGranted flips
    // false — so scope validators must key off `granted`, not `sitesLoaded`,
    // to avoid stripping a scoped route while the org catalog is denied.
    expect(screen.getByTestId("loaded").textContent).toBe("true");
    expect(screen.getByTestId("granted").textContent).toBe("false");
  });

  it("preserves the last-good list across a transient (non-permission) error", async () => {
    listSitesMock.mockImplementationOnce(async ({ onSuccess }) => onSuccess?.([makeSite(1)]));
    renderProvider();
    await waitFor(() => expect(screen.getByTestId("count").textContent).toBe("1"));

    listSitesMock.mockImplementationOnce(async ({ onError }) => onError?.("network blip", Code.Unavailable));
    fireEvent.click(screen.getByText("refetch"));

    await waitFor(() => expect(screen.getByTestId("error").textContent).toBe("network blip"));
    // Transient failures keep the last-good catalog visible.
    expect(screen.getByTestId("count").textContent).toBe("1");
    expect(screen.getByTestId("denied").textContent).toBe("false");
  });

  it("aborts the prior in-flight request before starting a new one", async () => {
    const signals: AbortSignal[] = [];
    // Never resolve, so both requests stay in flight and their signals persist.
    listSitesMock.mockImplementation(({ signal }: { signal?: AbortSignal }) => {
      if (signal) signals.push(signal);
      return new Promise<void>(() => {});
    });

    renderProvider();
    await waitFor(() => expect(signals).toHaveLength(1));

    act(() => {
      fireEvent.click(screen.getByText("refetch"));
    });

    await waitFor(() => expect(signals).toHaveLength(2));
    // The superseded request is aborted; a late response from it is ignored.
    expect(signals[0].aborted).toBe(true);
    expect(signals[1].aborted).toBe(false);
  });
});

// A consumer that opts into live polling for as long as it's mounted.
const Poller = () => {
  useSitesPolling();
  return null;
};

describe("SitesProvider polling", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    listSitesMock.mockImplementation(async ({ onSuccess }) => onSuccess?.([makeSite(1)]));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("is one-shot (no recurring poll) when no consumer opts in — e.g. header-only routes", async () => {
    render(
      <SitesProvider>
        <Probe />
      </SitesProvider>,
    );

    await vi.advanceTimersByTimeAsync(0); // flush the initial mount fetch
    expect(listSitesMock).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 3);
    // Still one call: the catalog does not poll without a registered consumer.
    expect(listSitesMock).toHaveBeenCalledTimes(1);
  });

  it("polls on the interval while a consumer registers via useSitesPolling", async () => {
    render(
      <SitesProvider>
        <Poller />
        <Probe />
      </SitesProvider>,
    );

    await vi.advanceTimersByTimeAsync(0);
    // Registering a poller must NOT trigger a second fetch on entry — only the
    // one-shot mount fetch has run so far.
    expect(listSitesMock).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    // The recurring refresh lands one interval later.
    expect(listSitesMock).toHaveBeenCalledTimes(2);
  });
});
