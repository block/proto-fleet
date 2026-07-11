import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import BuildingCard from "./BuildingCard";
import { BuildingSchema, BuildingWithCountsSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";

const statsMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/useBuildingStats", () => ({
  useBuildingStats: (...args: unknown[]) => statsMock(...args),
}));

interface RackHealthInit {
  rackId: number;
  rackLabel?: string;
  aisleIndex?: number;
  positionInAisle?: number;
  brokenCount?: number;
  offlineCount?: number;
  sleepingCount?: number;
  hashingCount?: number;
}

const rackHealth = (init: RackHealthInit) => ({
  rackId: BigInt(init.rackId),
  rackLabel: init.rackLabel ?? `R${init.rackId}`,
  aisleIndex: init.aisleIndex,
  positionInAisle: init.positionInAisle,
  hashingCount: init.hashingCount ?? 0,
  brokenCount: init.brokenCount ?? 0,
  offlineCount: init.offlineCount ?? 0,
  sleepingCount: init.sleepingCount ?? 0,
});

interface StatsOverrides {
  totalHashrateThs?: number;
  avgEfficiencyJth?: number;
  totalPowerKw?: number;
  reportingCount?: number;
  hashrateReportingCount?: number;
  efficiencyReportingCount?: number;
  powerReportingCount?: number;
  hashingCount?: number;
  brokenCount?: number;
  offlineCount?: number;
  sleepingCount?: number;
  rackHealth?: ReturnType<typeof rackHealth>[];
  rackCount?: number;
}

// Defaults assume "every reporting device has every field." Tests that
// exercise per-field missing-data behaviour override the individual
// counts.
const buildStats = (overrides: StatsOverrides = {}) => {
  const reporting = overrides.reportingCount ?? 0;
  return {
    buildingId: 7n,
    rackCount: 0,
    deviceCount: 0,
    reportingCount: reporting,
    hashrateReportingCount: reporting,
    efficiencyReportingCount: reporting,
    powerReportingCount: reporting,
    totalHashrateThs: 0,
    avgEfficiencyJth: 0,
    totalPowerKw: 0,
    hashingCount: 0,
    brokenCount: 0,
    offlineCount: 0,
    sleepingCount: 0,
    rackHealth: [] as ReturnType<typeof rackHealth>[],
    ...overrides,
  };
};

const LocationProbe = () => {
  const location = useLocation();
  return <div data-testid="location-probe">{`${location.pathname}${location.search}`}</div>;
};

interface RenderOptions {
  rackCount?: bigint;
  aisles?: number;
  racksPerAisle?: number;
  showMetrics?: boolean;
}

const renderCard = ({ rackCount = 24n, aisles = 2, racksPerAisle = 12, showMetrics }: RenderOptions = {}) => {
  const building = create(BuildingWithCountsSchema, {
    building: create(BuildingSchema, {
      id: 7n,
      name: "Building A",
      siteId: 1n,
      aisles,
      racksPerAisle,
    }),
    rackCount,
  });
  return render(
    <MemoryRouter initialEntries={["/sites"]}>
      <Routes>
        <Route
          path="*"
          element={
            <>
              <BuildingCard building={building} showMetrics={showMetrics} />
              <LocationProbe />
            </>
          }
        />
      </Routes>
    </MemoryRouter>,
  );
};

describe("BuildingCard", () => {
  it("shows skeletons in the status and footer slots while stats are loading", () => {
    statsMock.mockReturnValue({ stats: undefined, isLoading: true, hasLoaded: false, error: null, refetch: vi.fn() });
    renderCard({ rackCount: 3n });
    expect(screen.getByTestId("building-card-7-name")).toHaveTextContent("Building A");
    expect(screen.getByTestId("building-card-7-status").querySelector("[data-testid='skeleton-bar']")).not.toBeNull();
    expect(
      screen.getByTestId("building-card-7-stat-hashrate").querySelector("[data-testid='skeleton-bar']"),
    ).not.toBeNull();
  });

  it("renders one comma-joined clause per non-zero state bucket", () => {
    statsMock.mockReturnValue({
      stats: buildStats({
        brokenCount: 12,
        offlineCount: 9,
        sleepingCount: 49,
        hashingCount: 30,
        reportingCount: 100,
        rackHealth: [rackHealth({ rackId: 1, aisleIndex: 0, positionInAisle: 0, brokenCount: 12 })],
      }),
      isLoading: false,
      hasLoaded: true,
      error: null,
      refetch: vi.fn(),
    });
    renderCard({ rackCount: 20n });
    expect(screen.getByTestId("building-card-7-status")).toHaveTextContent("12 issues, 9 offline, 49 sleeping");
  });

  it("omits zero-count buckets from the status line", () => {
    statsMock.mockReturnValue({
      stats: buildStats({
        brokenCount: 0,
        offlineCount: 3,
        sleepingCount: 0,
        hashingCount: 47,
        reportingCount: 50,
        rackHealth: [rackHealth({ rackId: 1, aisleIndex: 0, positionInAisle: 0, offlineCount: 3 })],
      }),
      isLoading: false,
      hasLoaded: true,
      error: null,
      refetch: vi.fn(),
    });
    renderCard({ rackCount: 5n });
    const status = screen.getByTestId("building-card-7-status");
    expect(status).toHaveTextContent("3 offline");
    expect(status.textContent).not.toMatch(/issue|sleeping/);
  });

  it("shows 'All healthy' only when the building has assigned racks and zero issues", () => {
    statsMock.mockReturnValue({
      stats: buildStats({
        hashingCount: 50,
        reportingCount: 50,
        rackHealth: [rackHealth({ rackId: 1, aisleIndex: 0, positionInAisle: 0, hashingCount: 50 })],
      }),
      isLoading: false,
      hasLoaded: true,
      error: null,
      refetch: vi.fn(),
    });
    renderCard({ rackCount: 5n });
    expect(screen.getByTestId("building-card-7-status")).toHaveTextContent("All healthy");
  });

  it("renders an empty status line when no racks are assigned", () => {
    statsMock.mockReturnValue({
      stats: buildStats(),
      isLoading: false,
      hasLoaded: true,
      error: null,
      refetch: vi.fn(),
    });
    renderCard({ rackCount: 0n });
    expect(screen.getByTestId("building-card-7-status")).toHaveTextContent("");
  });

  it("renders formatted footer stats from the server roll-up", () => {
    statsMock.mockReturnValue({
      stats: buildStats({
        totalHashrateThs: 275_900, // → 275.9 PH/s
        totalPowerKw: 1_039_100, // → 1,039.1 MW
        avgEfficiencyJth: 3_766.4,
        reportingCount: 1000,
        rackCount: 20,
      }),
      isLoading: false,
      hasLoaded: true,
      error: null,
      refetch: vi.fn(),
    });
    renderCard({ rackCount: 20n });
    expect(screen.getByTestId("building-card-7-stat-hashrate")).toHaveTextContent("275.9 PH/s");
    expect(screen.getByTestId("building-card-7-stat-efficiency")).toHaveTextContent("3,766.4 J/TH");
    expect(screen.getByTestId("building-card-7-stat-power")).toHaveTextContent("1,039.1 MW");
  });

  it("renders em dashes in the footer when nothing is reporting", () => {
    statsMock.mockReturnValue({
      stats: buildStats({ reportingCount: 0 }),
      isLoading: false,
      hasLoaded: true,
      error: null,
      refetch: vi.fn(),
    });
    renderCard({ rackCount: 0n });
    expect(screen.getByTestId("building-card-7-stat-hashrate")).toHaveTextContent("—");
    expect(screen.getByTestId("building-card-7-stat-power")).toHaveTextContent("—");
    expect(screen.getByTestId("building-card-7-stat-efficiency")).toHaveTextContent("—");
  });

  it("hides the telemetry footer when showMetrics is false (dashboard card)", () => {
    statsMock.mockReturnValue({
      stats: buildStats({ totalHashrateThs: 275_900, reportingCount: 1000 }),
      isLoading: false,
      hasLoaded: true,
      error: null,
      refetch: vi.fn(),
    });
    renderCard({ rackCount: 20n, showMetrics: false });
    expect(screen.getByTestId("building-card-7-name")).toHaveTextContent("Building A");
    expect(screen.queryByTestId("building-card-7-stat-hashrate")).toBeNull();
    expect(screen.queryByTestId("building-card-7-stat-efficiency")).toBeNull();
    expect(screen.queryByTestId("building-card-7-stat-power")).toBeNull();
  });

  it("assigns heat bands based on issue ratio and marks empty positions as unassigned", () => {
    statsMock.mockReturnValue({
      stats: buildStats({
        rackHealth: [
          // R1 (0:0) — 1 broken out of 6 total → ~17% → band 3
          rackHealth({ rackId: 1, aisleIndex: 0, positionInAisle: 0, brokenCount: 1, hashingCount: 5 }),
          // R2 (0:1) — 3 sleeping out of 3 → 100% → band 5
          rackHealth({ rackId: 2, aisleIndex: 0, positionInAisle: 1, sleepingCount: 3 }),
          // R3 (1:0) — 3 issues out of 3 → 100% → band 5
          rackHealth({ rackId: 3, aisleIndex: 1, positionInAisle: 0, offlineCount: 2, sleepingCount: 1 }),
          // R4 (1:1) — 0 issues out of 4 → 0% → band 0
          rackHealth({ rackId: 4, aisleIndex: 1, positionInAisle: 1, hashingCount: 4 }),
        ],
      }),
      isLoading: false,
      hasLoaded: true,
      error: null,
      refetch: vi.fn(),
    });
    renderCard({ aisles: 2, racksPerAisle: 3 });
    const cells = screen.getByTestId("building-card-7-grid").querySelectorAll("span[aria-hidden='true']");
    expect(cells.length).toBe(6);
    expect(cells[0].getAttribute("data-heat-band")).toBe("3");
    expect(cells[1].getAttribute("data-heat-band")).toBe("5");
    expect(cells[2].getAttribute("data-heat-band")).toBe("unassigned");
    expect(cells[3].getAttribute("data-heat-band")).toBe("5");
    expect(cells[4].getAttribute("data-heat-band")).toBe("0");
    expect(cells[5].getAttribute("data-heat-band")).toBe("unassigned");
  });

  it("navigates to /buildings/:id when the card body is clicked", () => {
    statsMock.mockReturnValue({
      stats: buildStats(),
      isLoading: false,
      hasLoaded: true,
      error: null,
      refetch: vi.fn(),
    });
    renderCard({ rackCount: 0n });
    fireEvent.click(screen.getByTestId("building-card-7"));
    expect(screen.getByTestId("location-probe")).toHaveTextContent("/buildings/7");
  });

  it("opens an actions menu from the ellipsis trigger and routes to the chosen destination", () => {
    statsMock.mockReturnValue({
      stats: buildStats(),
      isLoading: false,
      hasLoaded: true,
      error: null,
      refetch: vi.fn(),
    });
    renderCard({ rackCount: 0n });
    expect(screen.queryByTestId("building-card-7-menu")).toBeNull();
    fireEvent.click(screen.getByTestId("building-card-7-menu-trigger"));
    expect(screen.getByTestId("building-card-7-menu")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("building-card-7-menu-racks"));
    expect(screen.getByTestId("location-probe")).toHaveTextContent("/racks?building=7");
  });

  it("dismisses the actions menu without navigating when the card background is clicked", () => {
    statsMock.mockReturnValue({
      stats: buildStats(),
      isLoading: false,
      hasLoaded: true,
      error: null,
      refetch: vi.fn(),
    });
    renderCard({ rackCount: 0n });
    fireEvent.click(screen.getByTestId("building-card-7-menu-trigger"));
    expect(screen.getByTestId("building-card-7-menu")).toBeInTheDocument();
    fireEvent.mouseDown(screen.getByTestId("building-card-7"));
    fireEvent.click(screen.getByTestId("building-card-7"));
    expect(screen.queryByTestId("building-card-7-menu")).toBeNull();
    expect(screen.getByTestId("location-probe")).toHaveTextContent("/sites");
  });

  it("dismisses the actions menu without navigating after a touch background dismiss", () => {
    statsMock.mockReturnValue({
      stats: buildStats(),
      isLoading: false,
      hasLoaded: true,
      error: null,
      refetch: vi.fn(),
    });
    renderCard({ rackCount: 0n });
    const card = screen.getByTestId("building-card-7");
    fireEvent.click(screen.getByTestId("building-card-7-menu-trigger"));
    expect(screen.getByTestId("building-card-7-menu")).toBeInTheDocument();
    fireEvent.touchStart(card);
    fireEvent.click(card);
    expect(screen.queryByTestId("building-card-7-menu")).toBeNull();
    expect(screen.getByTestId("location-probe")).toHaveTextContent("/sites");
  });

  it("dismisses the actions menu on Escape and restores card keyboard navigation", () => {
    statsMock.mockReturnValue({
      stats: buildStats(),
      isLoading: false,
      hasLoaded: true,
      error: null,
      refetch: vi.fn(),
    });
    renderCard({ rackCount: 0n });
    const card = screen.getByTestId("building-card-7");
    fireEvent.click(screen.getByTestId("building-card-7-menu-trigger"));
    expect(screen.getByTestId("building-card-7-menu")).toBeInTheDocument();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByTestId("building-card-7-menu")).toBeNull();
    fireEvent.keyDown(card, { key: "Enter" });
    expect(screen.getByTestId("location-probe")).toHaveTextContent("/buildings/7");
  });
});
