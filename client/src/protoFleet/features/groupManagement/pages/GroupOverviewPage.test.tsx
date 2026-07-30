import { MemoryRouter } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import GroupOverviewPage from "./GroupOverviewPage";
import { DeviceSetSchema, DeviceSetType } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const mockUseParams = vi.fn();
const mockNavigate = vi.fn();
const mockUseTelemetryMetrics = vi.fn();

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual<typeof import("react-router-dom")>("react-router-dom");
  return {
    ...actual,
    useParams: () => mockUseParams(),
  };
});

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({
    listGroups: ({ onSuccess }: { onSuccess: (groups: unknown[]) => void }) =>
      onSuccess([
        create(DeviceSetSchema, {
          id: 11n,
          type: DeviceSetType.GROUP,
          label: "Ops",
        }),
      ]),
    listGroupMembers: ({ onSuccess }: { onSuccess: (deviceIds: string[]) => void }) => onSuccess(["miner-a"]),
  }),
}));

vi.mock("@/protoFleet/api/useComponentErrors", () => ({
  useComponentErrors: () => ({
    controlBoardErrors: [],
    fanErrors: [],
    hashboardErrors: [],
    psuErrors: [],
  }),
}));

vi.mock("@/protoFleet/api/useDeviceSetStateCounts", () => ({
  useDeviceSetStateCounts: () => ({
    totalMiners: 1,
    stateCounts: {
      hashingCount: 1,
      brokenCount: 0,
      offlineCount: 0,
      sleepingCount: 0,
    },
    hasLoaded: true,
    refetch: vi.fn(),
  }),
}));

vi.mock("@/protoFleet/api/useTelemetryMetrics", () => ({
  useTelemetryMetrics: (options: unknown) => mockUseTelemetryMetrics(options),
}));

vi.mock("@/protoFleet/features/groupManagement/components/DeviceSetActionsMenu", () => ({
  __esModule: true,
  default: () => <div />,
}));

vi.mock("@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection", () => ({
  DeviceSetPerformanceSection: ({ className, gapClassName }: { className?: string; gapClassName?: string }) => (
    <div className={className} data-gap-class={gapClassName} data-testid="group-performance-grid">
      Performance section
    </div>
  ),
}));

vi.mock("@/protoFleet/features/groupManagement/components/FleetHealth", () => ({
  __esModule: true,
  default: () => <div>Fleet health</div>,
}));

vi.mock("@/protoFleet/features/groupManagement/components/GroupModal", () => ({
  __esModule: true,
  default: () => null,
}));

vi.mock("@/protoFleet/features/kpis/components/FleetErrors", () => ({
  __esModule: true,
  default: ({ gapClassName }: { gapClassName?: string }) => (
    <div data-gap-class={gapClassName} data-testid="group-fleet-errors">
      Fleet errors
    </div>
  ),
}));

vi.mock("@/protoFleet/store", () => ({
  useDuration: () => "12h",
  useSetDuration: () => vi.fn(),
}));

vi.mock("@/shared/assets/icons", () => ({
  ChevronDown: ({ className }: { className?: string }) => <svg aria-hidden="true" className={className} />,
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

vi.mock("@/shared/hooks/useNavigate", () => ({
  useNavigate: () => mockNavigate,
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

describe("GroupOverviewPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    mockUseParams.mockReturnValue({ groupLabel: "Ops" });
    useFleetStore.setState((state) => {
      state.ui.activeSite = DEFAULT_ACTIVE_SITE;
    });
    mockUseTelemetryMetrics.mockReturnValue({
      data: {
        metrics: [],
      },
    });
  });

  it("does not request uptime telemetry for group performance charts", async () => {
    render(
      <MemoryRouter>
        <GroupOverviewPage />
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(mockUseTelemetryMetrics).toHaveBeenCalledWith(
        expect.objectContaining({
          enabled: true,
          measurementTypes: expect.not.arrayContaining([MeasurementType.UPTIME]),
        }),
      ),
    );
  });

  it("uses the detail view spacing rhythm for sections and section content", async () => {
    render(
      <MemoryRouter>
        <GroupOverviewPage />
      </MemoryRouter>,
    );

    expect(await screen.findByTestId("group-overview-section")).toHaveClass("px-4", "pt-10", "laptop:px-8");
    expect(screen.getByTestId("group-overview-section").firstElementChild).toHaveClass(
      "gap-1",
      "overflow-visible",
      "p-2",
    );
    expect(screen.getByTestId("group-fleet-errors")).toHaveAttribute("data-gap-class", "gap-1");
    expect(screen.getByTestId("group-performance-section").querySelector(".sticky")).toHaveClass("pt-10", "pb-1");
    expect(screen.getByTestId("group-performance-grid")).toHaveClass("p-2");
    expect(screen.getByTestId("group-performance-grid")).toHaveAttribute("data-gap-class", "gap-1");
  });
});
