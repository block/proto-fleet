import { MemoryRouter } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import RackOverviewPage from "./RackOverviewPage";
import { DeviceSetSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";

const mockUseParams = vi.fn();
const mockNavigate = vi.fn();
const mockUseBuildings = vi.fn();
const mockUseDeviceSets = vi.fn();
const mockUseDeviceSetStateCounts = vi.fn();
const mockUseSites = vi.fn();
const mockUseTelemetryMetrics = vi.fn();
const mockUseComponentErrors = vi.fn();

const rackName = "Rack BA-Z01-R01";
const rackZone = "Building A";

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual<typeof import("react-router-dom")>("react-router-dom");
  return {
    ...actual,
    useParams: () => mockUseParams(),
  };
});

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => mockUseDeviceSets(),
}));

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => mockUseBuildings(),
}));

vi.mock("@/protoFleet/api/sites", () => ({
  useSites: () => mockUseSites(),
}));

vi.mock("@/protoFleet/api/useDeviceSetStateCounts", () => ({
  useDeviceSetStateCounts: () => mockUseDeviceSetStateCounts(),
}));

vi.mock("@/protoFleet/api/useTelemetryMetrics", () => ({
  useTelemetryMetrics: () => mockUseTelemetryMetrics(),
}));

vi.mock("@/protoFleet/api/useComponentErrors", () => ({
  useComponentErrors: () => mockUseComponentErrors(),
}));

vi.mock("@/shared/hooks/useNavigate", () => ({
  useNavigate: () => mockNavigate,
}));

vi.mock("@/protoFleet/store", () => ({
  useDuration: () => "12h",
  useSetDuration: () => vi.fn(),
}));

vi.mock("@/shared/hooks/useStickyState", () => ({
  useStickyState: () => ({
    refs: {
      vertical: {
        start: { current: null },
        end: { current: null },
      },
    },
  }),
}));

vi.mock("@/shared/assets/icons", () => ({
  ChevronDown: ({ className }: { className?: string }) => <svg aria-hidden="true" className={className} />,
}));

vi.mock("@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection", () => ({
  DeviceSetPerformanceSection: () => <div>Performance section</div>,
}));

vi.mock("@/protoFleet/features/groupManagement/components/DeviceSetActionsMenu", () => ({
  __esModule: true,
  default: () => <div />,
}));

vi.mock("@/protoFleet/features/kpis/components/FleetErrors", () => ({
  __esModule: true,
  default: () => <div>Fleet errors</div>,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/RackHealthModule", () => ({
  RackHealthModule: () => <div>Rack health</div>,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/ManageRackModal", () => ({
  ManageRackModal: () => null,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/ManageRackModal/SearchMinersModal", () => ({
  __esModule: true,
  default: () => null,
}));

vi.mock("@/shared/components/DurationSelector", () => ({
  __esModule: true,
  default: () => <div>Duration selector</div>,
  fleetDurations: [],
}));

vi.mock("@/shared/components/ProgressCircular", () => ({
  __esModule: true,
  default: () => <div>Loading</div>,
}));

const rack = create(DeviceSetSchema, {
  id: 7n,
  label: rackName,
  typeDetails: {
    case: "rackInfo",
    value: {
      rows: 6,
      columns: 5,
      zone: rackZone,
    },
  },
});

function mockResolvedRackPageData(
  deviceSet = rack,
  options: {
    allBuildings?: unknown[];
    sites?: unknown[];
    allRacks?: unknown[];
  } = {},
): void {
  mockUseParams.mockReturnValue({ rackId: "7" });
  mockUseBuildings.mockReturnValue({
    listAllBuildings: ({ onSuccess }: { onSuccess: (buildings: unknown[]) => void }) =>
      onSuccess(options.allBuildings ?? []),
  });
  mockUseDeviceSets.mockReturnValue({
    getDeviceSet: ({ onSuccess }: { onSuccess: (resolvedDeviceSet: typeof rack) => void }) => onSuccess(deviceSet),
    listGroupMembers: ({ onSuccess }: { onSuccess: (deviceIds: string[]) => void }) => onSuccess([]),
    assignDevicesToRack: vi.fn(),
    listRacks: ({ onSuccess }: { onSuccess: (racks: unknown[]) => void }) => onSuccess(options.allRacks ?? [deviceSet]),
    setRackSlotPosition: vi.fn(),
    deleteGroup: vi.fn(),
  });
  mockUseSites.mockReturnValue({
    listSites: ({ onSuccess }: { onSuccess: (sites: unknown[]) => void }) => onSuccess(options.sites ?? []),
  });
  mockUseDeviceSetStateCounts.mockReturnValue({
    stateCounts: {
      hashingCount: 0,
      brokenCount: 0,
      offlineCount: 0,
      sleepingCount: 0,
    },
    stats: {
      slotStatuses: [],
    },
    hasLoaded: true,
    refetch: vi.fn(),
  });
  mockUseTelemetryMetrics.mockReturnValue({
    data: {
      metrics: [],
    },
  });
  mockUseComponentErrors.mockReturnValue({
    controlBoardErrors: [],
    fanErrors: [],
    hashboardErrors: [],
    psuErrors: [],
  });
}

function renderRackOverviewPage() {
  return render(
    <MemoryRouter>
      <RackOverviewPage />
    </MemoryRouter>,
  );
}

describe("RackOverviewPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockResolvedRackPageData();
  });

  it("renders the rack zone as a subtitle under the rack name", async () => {
    renderRackOverviewPage();

    await waitFor(() => expect(screen.getAllByText(rackName).length).toBeGreaterThan(0));

    const zone = screen.getByText(rackZone);
    expect(zone).toBeVisible();
    expect(zone.className).toContain("text-text-primary");
    expect(screen.getByTestId("rack-page-breadcrumb")).toBeVisible();
    expect(screen.queryByTestId("header-icon-button")).not.toBeInTheDocument();
  });

  it("does not render a subtitle when the rack zone is empty", async () => {
    const rackWithoutZone = create(DeviceSetSchema, {
      id: 7n,
      label: rackName,
      typeDetails: {
        case: "rackInfo",
        value: {
          rows: 6,
          columns: 5,
          zone: "",
        },
      },
    });

    mockResolvedRackPageData(rackWithoutZone);

    renderRackOverviewPage();

    await waitFor(() => expect(screen.getAllByText(rackName).length).toBeGreaterThan(0));

    expect(screen.queryByText(rackZone)).not.toBeInTheDocument();
  });

  it("renders a switcher on the current rack breadcrumb item when other racks exist", async () => {
    const rackInBuilding = create(DeviceSetSchema, {
      id: 7n,
      label: rackName,
      typeDetails: {
        case: "rackInfo",
        value: {
          rows: 6,
          columns: 5,
          zone: rackZone,
          buildingId: 11n,
        },
      },
    });
    const siblingRackName = "Rack BA-Z01-R02";
    const siblingRack = create(DeviceSetSchema, {
      id: 8n,
      label: siblingRackName,
      typeDetails: {
        case: "rackInfo",
        value: {
          rows: 6,
          columns: 5,
          zone: rackZone,
        },
      },
    });

    mockResolvedRackPageData(rackInBuilding, {
      allBuildings: [
        {
          building: {
            id: 11n,
            siteId: 22n,
            name: "Building A",
          },
        },
      ],
      sites: [
        {
          site: {
            id: 22n,
            name: "Denver",
          },
        },
      ],
      allRacks: [rackInBuilding, siblingRack],
    });

    const user = userEvent.setup();
    renderRackOverviewPage();

    const switcher = await screen.findByTestId("rack-page-breadcrumb-switcher");
    expect(switcher).toHaveTextContent(rackName);

    await user.click(switcher);

    expect(screen.getByTestId(`rack-page-breadcrumb-menu-item-${siblingRackName}`)).toBeVisible();
  });
});
