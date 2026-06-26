import { BrowserRouter } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SiteResourcePanel from "./SiteResourcePanel";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type ActiveSite } from "@/protoFleet/store/types/activeSite";

// Controllable fixtures shared by the mocked data hooks.
const data = vi.hoisted(() => ({
  buildings: [] as { building?: { id: bigint; name: string } }[],
  buildingsError: null as string | null,
  racks: [] as { id: bigint; label: string }[],
  racksError: null as string | null,
}));
const refetchRacks = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({
    listBuildingsBySite: ({
      onSuccess,
      onError,
    }: {
      onSuccess?: (rows: BuildingWithCounts[]) => void;
      onError?: (msg: string) => void;
    }) => {
      if (data.buildingsError) onError?.(data.buildingsError);
      else onSuccess?.(data.buildings as unknown as BuildingWithCounts[]);
      return Promise.resolve();
    },
  }),
}));
vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({ listRacks: () => Promise.resolve() }),
}));
vi.mock("@/protoFleet/hooks/useDeviceSetListState", () => ({
  useDeviceSetListState: () => ({
    deviceSets: data.racks,
    statsMap: new Map(),
    isLoading: false,
    error: data.racksError,
    resetAndFetch: refetchRacks,
  }),
}));
vi.mock("@/protoFleet/api/useComponentErrors", () => ({
  useComponentErrors: () => ({ controlBoardErrors: 2, fanErrors: 0, hashboardErrors: 1, psuErrors: 0 }),
}));
vi.mock("@/protoFleet/store", async (importActual) => ({
  ...(await importActual<typeof import("@/protoFleet/store")>()),
  useTemperatureUnit: () => "C",
}));
// Card internals (their own stats hooks) aren't under test here — stub to a
// label so we can assert which gallery is rendered.
vi.mock("@/protoFleet/features/buildings/components/BuildingCard", () => ({
  default: ({ building }: { building: BuildingWithCounts }) => (
    <div data-testid="building-card">{building.building?.name}</div>
  ),
}));
vi.mock("@/protoFleet/features/fleetManagement/components/RackCard", () => ({
  RackCard: ({ label }: { label: string }) => <div data-testid="rack-card">{label}</div>,
}));
vi.mock("@/protoFleet/features/fleetManagement/utils/rackCardMapper", () => ({
  mapRackToCardProps: () => ({ cols: 1, rows: 1, slots: [], statusSegments: [] }),
}));

const ACTIVE_SITE: ActiveSite = { kind: "site", id: "8", slug: "austin" };

const renderPanel = () =>
  render(
    <BrowserRouter>
      <SiteResourcePanel siteId={8n} activeSite={ACTIVE_SITE} />
    </BrowserRouter>,
  );

describe("SiteResourcePanel", () => {
  beforeEach(() => {
    data.buildings = [{ building: { id: 1n, name: "North Hall" } }];
    data.buildingsError = null;
    data.racks = [{ id: 10n, label: "Rack A" }];
    data.racksError = null;
    refetchRacks.mockClear();
  });

  it("defaults to the Buildings gallery", () => {
    renderPanel();
    expect(screen.getByTestId("building-card")).toHaveTextContent("North Hall");
    expect(screen.queryByTestId("rack-card")).not.toBeInTheDocument();
  });

  it("switches to the Racks gallery", () => {
    renderPanel();
    fireEvent.click(screen.getByText("Racks"));
    expect(screen.getByTestId("rack-card")).toHaveTextContent("Rack A");
    expect(screen.queryByTestId("building-card")).not.toBeInTheDocument();
  });

  it("switches to the Components breakdown (FleetErrors)", () => {
    renderPanel();
    fireEvent.click(screen.getByText("Components"));
    expect(screen.getByText("Control Boards")).toBeInTheDocument();
    expect(screen.getByText("Power supplies")).toBeInTheDocument();
    expect(screen.queryByTestId("building-card")).not.toBeInTheDocument();
  });

  it("shows an empty state when the site has no buildings", () => {
    data.buildings = [];
    renderPanel();
    expect(screen.getByText("No buildings in this site yet.")).toBeInTheDocument();
  });

  it("surfaces a building-list error with retry instead of an empty state", () => {
    data.buildings = [];
    data.buildingsError = "boom";
    renderPanel();
    expect(screen.getByTestId("site-resource-error")).toHaveTextContent("Couldn't load buildings.");
    expect(screen.queryByText("No buildings in this site yet.")).not.toBeInTheDocument();
    expect(screen.queryByTestId("building-card")).not.toBeInTheDocument();
  });

  it("surfaces a rack-list error with a working retry", () => {
    data.racks = [];
    data.racksError = "boom";
    renderPanel();
    fireEvent.click(screen.getByText("Racks"));
    expect(screen.getByTestId("site-resource-error")).toHaveTextContent("Couldn't load racks.");
    fireEvent.click(screen.getByText("Retry"));
    expect(refetchRacks).toHaveBeenCalled();
  });

  it("points View all at the matching site-scoped fleet page per tab", () => {
    renderPanel();
    // Buildings
    expect(screen.getByTestId("site-resource-view-all")).toHaveAttribute("href", "/austin/fleet/buildings");
    // Racks
    fireEvent.click(screen.getByText("Racks"));
    expect(screen.getByTestId("site-resource-view-all")).toHaveAttribute("href", "/austin/fleet/racks");
    // Components → miners with a status filter applied
    fireEvent.click(screen.getByText("Components"));
    expect(screen.getByTestId("site-resource-view-all").getAttribute("href")).toMatch(/^\/austin\/fleet\/miners\?/);
  });
});
