import { BrowserRouter } from "react-router-dom";
import { render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SiteCard from "./SiteCard";
import { type GetSiteStatsResponse } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";

// Drive the per-card roll-up deterministically and pin the temperature unit
// so the range string is stable across environments.
const mockStats = vi.hoisted(() => ({ current: undefined as GetSiteStatsResponse | undefined }));
vi.mock("@/protoFleet/api/useSiteStats", () => ({
  useSiteStats: () => ({ stats: mockStats.current }),
}));
vi.mock("@/protoFleet/store", async (importActual) => ({
  ...(await importActual<typeof import("@/protoFleet/store")>()),
  useTemperatureUnit: () => "C",
}));

const makeSite = (id: number, name: string, slug: string) =>
  ({ site: { id: BigInt(id), name, slug } }) as unknown as SiteWithCounts;

const makeStats = (overrides: Partial<GetSiteStatsResponse> = {}) =>
  ({
    hashingCount: 80,
    brokenCount: 0,
    offlineCount: 0,
    sleepingCount: 0,
    totalHashrateThs: 922_600,
    hashrateReportingCount: 80,
    totalPowerKw: 3_500,
    powerReportingCount: 80,
    minTemperatureC: 30,
    maxTemperatureC: 60,
    temperatureReportingCount: 80,
    ...overrides,
  }) as unknown as GetSiteStatsResponse;

const renderCard = (site: SiteWithCounts) =>
  render(
    <BrowserRouter>
      <SiteCard site={site} />
    </BrowserRouter>,
  );

describe("SiteCard", () => {
  beforeEach(() => {
    mockStats.current = undefined;
  });

  it("links the arrow action to the site detail page", () => {
    mockStats.current = makeStats();
    renderCard(makeSite(7, "Austin", "austin"));
    expect(screen.getByTestId("dashboard-site-card-7-detail")).toHaveAttribute("href", "/sites/7");
  });

  it("renders dynamic-unit hashrate and the temperature range", () => {
    mockStats.current = makeStats();
    renderCard(makeSite(7, "Austin", "austin"));

    expect(within(screen.getByTestId("dashboard-site-card-7-hashrate")).getByText("922.6 PH/s")).toBeInTheDocument();
    expect(within(screen.getByTestId("dashboard-site-card-7-power")).getByText("3.5 MW")).toBeInTheDocument();
    expect(screen.getByTestId("dashboard-site-card-7-temperature")).toHaveTextContent("30–60 °C");
  });

  it("renders the health bar once stats load", () => {
    mockStats.current = makeStats();
    renderCard(makeSite(7, "Austin", "austin"));

    expect(screen.getByTestId("dashboard-site-card-7-health")).toBeInTheDocument();
  });

  it("renders a scoped, status-filtered needs-attention badge when miners need attention", () => {
    mockStats.current = makeStats({ hashingCount: 80, brokenCount: 20 });
    renderCard(makeSite(7, "Austin", "austin"));

    const badge = screen.getByTestId("dashboard-site-card-7-needs-attention");
    expect(badge).toHaveTextContent("20% need attention");
    expect(badge).toHaveAttribute("href", expect.stringContaining("/austin/fleet/miners?"));
  });

  it("hides the needs-attention badge when nothing needs attention", () => {
    mockStats.current = makeStats({ brokenCount: 0 });
    renderCard(makeSite(7, "Austin", "austin"));
    expect(screen.queryByTestId("dashboard-site-card-7-needs-attention")).not.toBeInTheDocument();
  });

  it("shows skeletons while stats load", () => {
    mockStats.current = undefined;
    renderCard(makeSite(7, "Austin", "austin"));

    // 3 metric tiles + the health bar all render skeletons.
    expect(screen.getAllByTestId("skeleton-bar")).toHaveLength(4);
    expect(screen.queryByTestId("dashboard-site-card-7-health")).not.toBeInTheDocument();
  });
});
