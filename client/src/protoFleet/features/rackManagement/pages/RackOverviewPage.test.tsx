import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import RackOverviewPage from "./RackOverviewPage";
import { DeviceSetSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";

const mockUseParams = vi.fn();
const mockNavigate = vi.fn();
const mockUseDeviceSets = vi.fn();
const mockUseDeviceSetStateCounts = vi.fn();
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

vi.mock("@/protoFleet/features/rackManagement/components/RackHealthModule", () => ({
  RackHealthModule: () => <div>Rack health</div>,
}));

vi.mock("@/protoFleet/features/rackManagement/components/AssignMinersModal", () => ({
  AssignMinersModal: () => null,
}));

vi.mock("@/protoFleet/features/rackManagement/components/AssignMinersModal/SearchMinersModal", () => ({
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

function mockResolvedRackPageData(deviceSet = rack): void {
  mockUseParams.mockReturnValue({ rackId: "7" });
  mockUseDeviceSets.mockReturnValue({
    getDeviceSet: ({ onSuccess }: { onSuccess: (resolvedDeviceSet: typeof rack) => void }) => onSuccess(deviceSet),
    listGroupMembers: ({ onSuccess }: { onSuccess: (deviceIds: string[]) => void }) => onSuccess([]),
    addDevicesToDeviceSet: vi.fn(),
    setRackSlotPosition: vi.fn(),
    deleteGroup: vi.fn(),
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

describe("RackOverviewPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockResolvedRackPageData();
  });

  it("renders the rack zone as a subtitle under the rack name", async () => {
    render(<RackOverviewPage />);

    await waitFor(() => expect(screen.getByText(rackName)).toBeVisible());

    const zone = screen.getByText(rackZone);
    expect(zone).toBeVisible();
    expect(zone.className).toContain("text-text-primary");
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

    render(<RackOverviewPage />);

    await waitFor(() => expect(screen.getByText(rackName)).toBeVisible());

    expect(screen.queryByText(rackZone)).not.toBeInTheDocument();
  });
});
