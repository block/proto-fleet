import { MemoryRouter } from "react-router-dom";
import { act, render, screen, waitFor } from "@testing-library/react";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import CohortOverviewPage from "./CohortOverviewPage";
import {
  CohortDeviceSchema,
  CohortDeviceDisplaySchema,
  CohortFirmwareTargetSchema,
  CohortMemberSchema,
  CohortSchema,
  CohortState,
  CohortSummarySchema,
  type Cohort,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import type { FleetDuration } from "@/shared/components/DurationSelector";

const mocks = vi.hoisted(() => ({
  getCohort: vi.fn(),
  addDevices: vi.fn(),
  listAllDevices: vi.fn(),
  listSites: vi.fn(),
  listFirmwareFiles: vi.fn(),
  useTelemetryMetrics: vi.fn(),
  useParams: vi.fn(),
  navigate: vi.fn(),
  routeSiteScope: undefined as unknown,
  duration: "24h" as FleetDuration,
  setDuration: vi.fn(),
}));

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual<typeof import("react-router-dom")>("react-router-dom");
  return {
    ...actual,
    useParams: () => mocks.useParams(),
  };
});

vi.mock("@/protoFleet/api/useCohortApi", () => ({
  useCohortApi: () => ({
    getCohort: mocks.getCohort,
    extendCohort: vi.fn(),
    setDesiredFirmware: vi.fn(),
    addDevices: mocks.addDevices,
    removeDevices: vi.fn(),
    releaseCohort: vi.fn(),
    adminReassign: vi.fn(),
    listAllDevices: mocks.listAllDevices,
  }),
}));

vi.mock("@/protoFleet/components/MinerSelectionList", async () => {
  const React = await import("react");
  interface MockMinerSelectionListProps {
    visibleTotal?: number;
    isRowVisible?: (item: {
      deviceIdentifier: string;
      name: string;
      manufacturer: string;
      model: string;
      ipAddress: string;
      rackLabel: string;
      groupLabels: string[];
    }) => boolean;
  }
  const MockMinerSelectionList = React.forwardRef<unknown, MockMinerSelectionListProps>((props, ref) => {
    React.useImperativeHandle(ref, () => ({
      getSelection: () => ({
        selectedItems: ["eligible-1"],
        allSelected: false,
        totalMiners: props.visibleTotal,
        filter: {},
      }),
    }));

    const eligibleVisible =
      props.isRowVisible?.({
        deviceIdentifier: "eligible-1",
        name: "Eligible miner",
        manufacturer: "Proto",
        model: "Rig",
        ipAddress: "",
        rackLabel: "",
        groupLabels: [],
      }) ?? true;
    const ineligibleVisible =
      props.isRowVisible?.({
        deviceIdentifier: "reserved-1",
        name: "Reserved miner",
        manufacturer: "Proto",
        model: "Rig",
        ipAddress: "",
        rackLabel: "",
        groupLabels: [],
      }) ?? true;

    return (
      <div data-testid="miner-selection-list" data-visible-total={props.visibleTotal ?? ""}>
        <span>{eligibleVisible ? "eligible-visible" : "eligible-hidden"}</span>
        <span>{ineligibleVisible ? "reserved-visible" : "reserved-hidden"}</span>
      </div>
    );
  });
  MockMinerSelectionList.displayName = "MockMinerSelectionList";
  return { default: MockMinerSelectionList };
});

vi.mock("@/protoFleet/api/useFirmwareApi", () => ({
  useFirmwareApi: () => ({
    listFirmwareFiles: mocks.listFirmwareFiles,
  }),
}));

vi.mock("@/protoFleet/api/sites", () => ({
  useSites: () => ({
    listSites: mocks.listSites,
  }),
}));

vi.mock("@/protoFleet/api/useTelemetryMetrics", () => ({
  useTelemetryMetrics: (options: unknown) => mocks.useTelemetryMetrics(options),
}));

vi.mock("@/protoFleet/routing/siteScope", () => ({
  scopedPath: (path: string) => path,
  useRouteSiteScope: () => mocks.routeSiteScope,
}));

vi.mock("@/shared/hooks/useNavigate", () => ({
  useNavigate: () => mocks.navigate,
}));

vi.mock("@/protoFleet/store", () => ({
  useDuration: () => mocks.duration,
  useSetDuration: () => mocks.setDuration,
  useRole: () => "USER",
  useUsername: () => "owner",
}));

vi.mock("@/protoFleet/features/dashboard/components/HashratePanel", () => ({
  HashratePanel: () => <div data-testid="hashrate-panel">Hashrate panel</div>,
}));

vi.mock("@/protoFleet/features/dashboard/components/UptimePanel", () => ({
  UptimePanel: () => <div data-testid="uptime-panel">Uptime panel</div>,
}));

vi.mock("@/protoFleet/features/dashboard/components/TemperaturePanel", () => ({
  TemperaturePanel: () => <div data-testid="temperature-panel">Temperature panel</div>,
}));

vi.mock("@/protoFleet/features/dashboard/components/PowerPanel", () => ({
  PowerPanel: ({ totalMiners }: { totalMiners: number }) => (
    <div data-testid="power-panel">Power panel for {totalMiners}</div>
  ),
}));

vi.mock("@/protoFleet/features/dashboard/components/EfficiencyPanel", () => ({
  EfficiencyPanel: ({ totalMiners }: { totalMiners: number }) => (
    <div data-testid="efficiency-panel">Efficiency panel for {totalMiners}</div>
  ),
}));

const buildCohort = ({
  isDefault = false,
  deviceIdentifiers = ["miner-001", "miner-002"],
  firmwareVersions = ["1.3.6", "1.3.5"],
  firmwareFileId = "",
}: {
  isDefault?: boolean;
  deviceIdentifiers?: string[];
  firmwareVersions?: string[];
  firmwareFileId?: string;
} = {}): Cohort =>
  create(CohortSchema, {
    summary: create(CohortSummarySchema, {
      id: 7n,
      label: isDefault ? "Default cohort" : "Release cohort",
      isDefault,
      ownerUsername: "owner",
      state: CohortState.ACTIVE,
      purpose: "Firmware validation",
      sourceActorType: "user",
      explicitMemberCount: BigInt(deviceIdentifiers.length),
    }),
    members: deviceIdentifiers.map((deviceIdentifier, index) =>
      create(CohortMemberSchema, {
        cohortId: 7n,
        deviceIdentifier,
        display: create(CohortDeviceDisplaySchema, {
          name: deviceIdentifier,
          manufacturer: "Proto",
          model: "Rig",
          firmwareVersion: firmwareVersions[index] ?? "",
        }),
      }),
    ),
    firmwareTargets: firmwareFileId
      ? [
          create(CohortFirmwareTargetSchema, {
            manufacturer: "Proto",
            model: "Rig",
            firmwareFileId,
          }),
        ]
      : [],
  });

const renderPage = () =>
  render(
    <MemoryRouter>
      <CohortOverviewPage />
    </MemoryRouter>,
  );

describe("CohortOverviewPage performance graphs", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.useParams.mockReturnValue({ cohortId: "7" });
    mocks.routeSiteScope = undefined;
    mocks.duration = "24h";
    mocks.listFirmwareFiles.mockResolvedValue([]);
    mocks.listSites.mockImplementation(({ onSuccess }) => {
      onSuccess([]);
      return Promise.resolve([]);
    });
    mocks.addDevices.mockResolvedValue(buildCohort({ deviceIdentifiers: ["miner-001", "miner-002", "eligible-1"] }));
    mocks.listAllDevices.mockResolvedValue([
      create(CohortDeviceSchema, {
        deviceIdentifier: "eligible-1",
        siteId: 1n,
        display: create(CohortDeviceDisplaySchema, {
          name: "eligible-1",
          manufacturer: "Proto",
          model: "Rig",
        }),
      }),
      create(CohortDeviceSchema, {
        deviceIdentifier: "eligible-2",
        siteId: 1n,
        display: create(CohortDeviceDisplaySchema, {
          name: "eligible-2",
          manufacturer: "Proto",
          model: "Rig",
        }),
      }),
    ]);
    mocks.useTelemetryMetrics.mockReturnValue({ data: { metrics: [] }, error: null });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders performance graphs for non-default cohorts scoped to explicit members", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort());

    renderPage();

    expect(await screen.findByTestId("cohort-performance-section")).toBeInTheDocument();
    expect(screen.getByTestId("hashrate-panel")).toBeInTheDocument();
    expect(screen.getByTestId("uptime-panel")).toBeInTheDocument();
    expect(screen.getByTestId("temperature-panel")).toBeInTheDocument();
    expect(screen.getByTestId("power-panel")).toHaveTextContent("Power panel for 2");
    expect(screen.getByTestId("efficiency-panel")).toHaveTextContent("Efficiency panel for 2");

    expect(mocks.useTelemetryMetrics).toHaveBeenCalledWith(
      expect.objectContaining({
        deviceIds: ["miner-001", "miner-002"],
        measurementTypes: [
          MeasurementType.HASHRATE,
          MeasurementType.POWER,
          MeasurementType.TEMPERATURE,
          MeasurementType.EFFICIENCY,
          MeasurementType.UPTIME,
        ],
        duration: "24h",
        enabled: true,
      }),
    );
  });

  it("shows member firmware versions and desired firmware status", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort({ firmwareFileId: "fw-1" }));
    mocks.listFirmwareFiles.mockResolvedValue([
      {
        id: "fw-1",
        filename: "proto-rig.swu",
        size: 100,
        uploaded_at: "2026-06-30T20:00:00Z",
        target_manufacturer: "Proto",
        target_model: "Rig",
        firmware_version: "1.3.6",
      },
    ]);

    renderPage();

    expect(await screen.findByText("Firmware versions")).toBeInTheDocument();
    expect(screen.getAllByText("1.3.6").length).toBeGreaterThan(0);
    expect(screen.getAllByText("1.3.5").length).toBeGreaterThan(0);
    expect(screen.getByText("On target")).toBeInTheDocument();
    expect(screen.getByText("Target: 1.3.6")).toBeInTheDocument();
  });

  it("refreshes cohort details while firmware members are off target", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mocks.getCohort
      .mockResolvedValueOnce(
        buildCohort({ deviceIdentifiers: ["miner-001"], firmwareVersions: ["1.3.5"], firmwareFileId: "fw-1" }),
      )
      .mockResolvedValue(
        buildCohort({ deviceIdentifiers: ["miner-001"], firmwareVersions: ["1.3.6"], firmwareFileId: "fw-1" }),
      );
    mocks.listFirmwareFiles.mockResolvedValue([
      {
        id: "fw-1",
        filename: "proto-rig.swu",
        size: 100,
        uploaded_at: "2026-06-30T20:00:00Z",
        target_manufacturer: "Proto",
        target_model: "Rig",
        firmware_version: "1.3.6",
      },
    ]);

    renderPage();

    expect(await screen.findByText("Target: 1.3.6")).toBeInTheDocument();
    expect(screen.getByText("0/1 on target")).toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });

    await waitFor(() => expect(mocks.getCohort).toHaveBeenCalledTimes(2));
    expect(screen.getByText("All on target")).toBeInTheDocument();
  });

  it("does not render performance graphs for the default cohort", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort({ isDefault: true }));

    renderPage();

    await screen.findByText("Default cohort");
    expect(screen.queryByTestId("cohort-performance-section")).not.toBeInTheDocument();
    expect(mocks.useTelemetryMetrics).not.toHaveBeenCalled();
  });

  it("does not request telemetry for empty non-default cohorts", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort({ deviceIdentifiers: [] }));

    renderPage();

    expect(await screen.findByTestId("cohort-performance-section")).toBeInTheDocument();
    expect(mocks.useTelemetryMetrics).toHaveBeenCalledWith(
      expect.objectContaining({
        deviceIds: [],
        enabled: false,
      }),
    );
  });

  it("does not apply route site scope to cohort performance telemetry", async () => {
    mocks.routeSiteScope = { kind: "site", id: "42", label: "Austin" };
    mocks.getCohort.mockResolvedValue(buildCohort());

    renderPage();

    await screen.findByTestId("cohort-performance-section");
    const telemetryOptions = mocks.useTelemetryMetrics.mock.calls[mocks.useTelemetryMetrics.mock.calls.length - 1]?.[0];
    expect(telemetryOptions).toEqual(
      expect.objectContaining({
        deviceIds: ["miner-001", "miner-002"],
      }),
    );
    expect(telemetryOptions).not.toHaveProperty("siteIds");
    expect(telemetryOptions).not.toHaveProperty("includeUnassigned");
  });

  it("passes updated duration into the telemetry request", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort());
    const { rerender } = renderPage();

    await screen.findByTestId("cohort-performance-section");
    expect(mocks.useTelemetryMetrics.mock.calls[mocks.useTelemetryMetrics.mock.calls.length - 1]?.[0]).toEqual(
      expect.objectContaining({ duration: "24h" }),
    );

    mocks.duration = "7d";
    rerender(
      <MemoryRouter>
        <CohortOverviewPage />
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(mocks.useTelemetryMetrics.mock.calls[mocks.useTelemetryMetrics.mock.calls.length - 1]?.[0]).toEqual(
        expect.objectContaining({ duration: "7d" }),
      ),
    );
  });

  it("allows the duration selector to update the fleet duration", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort());
    renderPage();

    await screen.findByTestId("cohort-performance-section");
    await userEvent.click(screen.getByRole("button", { name: "7d" }));

    expect(mocks.setDuration).toHaveBeenCalledWith("7d");
  });

  it("shows a local performance error without replacing the cohort page", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort());
    mocks.useTelemetryMetrics.mockReturnValue({ data: null, error: new Error("failed") });

    renderPage();

    expect(await screen.findByTestId("cohort-performance-section")).toBeInTheDocument();
    expect(screen.getByText("Couldn't load cohort performance")).toBeInTheDocument();
    expect(screen.getAllByText("Members").length).toBeGreaterThan(0);
  });

  it("adds a general count of eligible rigs", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort());
    renderPage();

    await screen.findByText("Release cohort");
    await userEvent.click(screen.getByRole("button", { name: "Add" }));

    expect(await screen.findByText("Eligible miners")).toBeInTheDocument();
    await waitFor(() => expect(screen.getAllByText("2").length).toBeGreaterThanOrEqual(2));

    await userEvent.clear(screen.getByLabelText("Count"));
    await userEvent.type(screen.getByLabelText("Count"), "2");
    const addButtons = screen.getAllByRole("button", { name: "Add" });
    await userEvent.click(addButtons[addButtons.length - 1]);

    expect(mocks.listAllDevices).toHaveBeenCalledWith(
      expect.objectContaining({
        filter: expect.objectContaining({
          manufacturers: ["Proto"],
          models: ["Rig"],
        }),
      }),
    );
    expect(mocks.addDevices).toHaveBeenCalledWith(
      expect.objectContaining({
        cohortId: 7n,
        deviceIdentifiers: ["eligible-1", "eligible-2"],
      }),
    );
  });

  it("filters explicit add options to eligible rigs only", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort());
    renderPage();

    await screen.findByText("Release cohort");
    await userEvent.click(screen.getByRole("button", { name: "Add" }));
    await waitFor(() => expect(screen.getAllByText("2").length).toBeGreaterThanOrEqual(2));

    await userEvent.click(screen.getByRole("button", { name: "Add members" }));
    await userEvent.click(await screen.findByText("Selected miners"));

    expect(await screen.findByTestId("miner-selection-list")).toHaveAttribute("data-visible-total", "2");
    expect(screen.getByText("eligible-visible")).toBeInTheDocument();
    expect(screen.getByText("reserved-hidden")).toBeInTheDocument();
  });
});
