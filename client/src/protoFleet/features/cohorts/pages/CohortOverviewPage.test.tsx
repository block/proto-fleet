import { MemoryRouter } from "react-router-dom";
import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import userEvent from "@testing-library/user-event";

import CohortOverviewPage from "./CohortOverviewPage";
import {
  type Cohort,
  CohortConfigDimension,
  CohortConfigProgressSchema,
  CohortDesiredConfigSchema,
  CohortDeviceDisplaySchema,
  CohortDeviceSchema,
  CohortFirmwareProgressSchema,
  CohortFirmwareRolloutState,
  CohortFirmwareStatusSchema,
  CohortFirmwareTargetSchema,
  CohortFirmwareValidationBaselineSchema,
  CohortFirmwareValidationMetricSchema,
  CohortFirmwareValidationPointSchema,
  CohortFirmwareValidationState,
  CohortFirmwareValidationWindow,
  CohortFirmwareVersionCountSchema,
  CohortFirmwareVersionHistoryPointSchema,
  CohortMemberSchema,
  CohortPoolDesiredConfigSchema,
  CohortSchema,
  CohortState,
  CohortSummarySchema,
  GetCohortFirmwareValidationResponseSchema,
  GetCohortFirmwareVersionHistoryResponseSchema,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import type { FleetDuration } from "@/shared/components/DurationSelector";

const mocks = vi.hoisted(() => ({
  getCohort: vi.fn(),
  getFirmwareVersionHistory: vi.fn(),
  getFirmwareValidation: vi.fn(),
  addDevices: vi.fn(),
  listDevices: vi.fn(),
  listAllDevices: vi.fn(),
  listFirmwareFiles: vi.fn(),
  useTelemetryMetrics: vi.fn(),
  useParams: vi.fn(),
  navigate: vi.fn(),
  routeSiteScope: undefined as unknown,
  duration: "24h" as FleetDuration,
  setDuration: vi.fn(),
  miningPools: [] as Array<{ poolId: string; name: string; poolUrl: string; username: string }>,
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
    getFirmwareVersionHistory: mocks.getFirmwareVersionHistory,
    getFirmwareValidation: mocks.getFirmwareValidation,
    extendCohort: vi.fn(),
    setDesiredFirmware: vi.fn(),
    setDesiredPools: vi.fn(),
    addDevices: mocks.addDevices,
    removeDevices: vi.fn(),
    releaseCohort: vi.fn(),
    adminReassign: vi.fn(),
    listDevices: mocks.listDevices,
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

vi.mock("@/protoFleet/api/usePools", () => ({
  default: () => ({ miningPools: mocks.miningPools, isLoading: false }),
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
  poolIds = [],
}: {
  isDefault?: boolean;
  deviceIdentifiers?: string[];
  firmwareVersions?: string[];
  firmwareFileId?: string;
  poolIds?: bigint[];
} = {}): Cohort => {
  const targetVersion = "1.3.6";
  const firmwareStatuses = deviceIdentifiers.map((_, index) => {
    const currentFirmwareVersion = firmwareVersions[index] ?? "";
    const complete = Boolean(firmwareFileId) && currentFirmwareVersion === targetVersion;
    return firmwareFileId
      ? create(CohortFirmwareStatusSchema, {
          targetFirmwareFileId: firmwareFileId,
          targetFirmwareVersion: targetVersion,
          currentFirmwareVersion,
          state: complete ? CohortFirmwareRolloutState.COMPLETE : CohortFirmwareRolloutState.VERIFYING,
        })
      : undefined;
  });
  const completeCount = firmwareStatuses.filter(
    (status) => status?.state === CohortFirmwareRolloutState.COMPLETE,
  ).length;
  const targetedCount = firmwareStatuses.filter(Boolean).length;

  return create(CohortSchema, {
    summary: create(CohortSummarySchema, {
      id: 7n,
      label: isDefault ? "Default cohort" : "Release cohort",
      isDefault,
      ownerUsername: "owner",
      state: CohortState.ACTIVE,
      purpose: "Firmware validation",
      sourceActorType: "user",
      explicitMemberCount: BigInt(deviceIdentifiers.length),
      desiredConfig:
        poolIds.length > 0
          ? create(CohortDesiredConfigSchema, {
              pools: create(CohortPoolDesiredConfigSchema, {
                primaryPoolId: poolIds[0],
                backup1PoolId: poolIds[1],
                backup2PoolId: poolIds[2],
              }),
            })
          : undefined,
      firmwareProgress: firmwareFileId
        ? create(CohortFirmwareProgressSchema, {
            targetedCount,
            completeCount,
            verifyingCount: targetedCount - completeCount,
          })
        : undefined,
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
        firmwareStatus: firmwareStatuses[index],
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
};

const historyTimestamp = (milliseconds: number) =>
  create(TimestampSchema, {
    seconds: BigInt(Math.floor(milliseconds / 1000)),
    nanos: (milliseconds % 1000) * 1_000_000,
  });

const buildFirmwareHistory = () => {
  const now = Date.now();
  return create(GetCohortFirmwareVersionHistoryResponseSchema, {
    memberCount: 2,
    points: [
      create(CohortFirmwareVersionHistoryPointSchema, {
        timestamp: historyTimestamp(now - 60_000),
        versions: [create(CohortFirmwareVersionCountSchema, { firmwareVersion: "1.3.5", deviceCount: 2 })],
      }),
      create(CohortFirmwareVersionHistoryPointSchema, {
        timestamp: historyTimestamp(now),
        versions: [
          create(CohortFirmwareVersionCountSchema, { firmwareVersion: "1.3.6", deviceCount: 1 }),
          create(CohortFirmwareVersionCountSchema, { firmwareVersion: "1.3.5", deviceCount: 1 }),
        ],
      }),
    ],
  });
};

const buildFirmwareValidation = () => {
  const now = Date.now();
  const metric = (measurementType: MeasurementType, baselineValue: number, targetValue: number) =>
    create(CohortFirmwareValidationMetricSchema, {
      measurementType,
      baselineAverage: baselineValue,
      targetAverage: targetValue,
      absoluteDelta: targetValue - baselineValue,
      percentageDelta: 10,
      baselineReportingDeviceCount: 1,
      targetReportingDeviceCount: 1,
      baselinePoints: [
        create(CohortFirmwareValidationPointSchema, {
          elapsed: { seconds: 0n, nanos: 0 },
          value: baselineValue,
          deviceCount: 1,
        }),
      ],
      targetPoints: [
        create(CohortFirmwareValidationPointSchema, {
          elapsed: { seconds: 0n, nanos: 0 },
          value: targetValue,
          deviceCount: 1,
        }),
      ],
    });

  return create(GetCohortFirmwareValidationResponseSchema, {
    state: CohortFirmwareValidationState.AVAILABLE,
    manufacturer: "Proto",
    model: "Rig",
    targetFirmwareFileId: "fw-1",
    targetFirmwareVersion: "1.3.6",
    rolloutStartedAt: historyTimestamp(now - 24 * 60 * 60 * 1000),
    comparisonWindow: CohortFirmwareValidationWindow.SIX_HOURS,
    stabilizationGap: { seconds: 1_800n, nanos: 0 },
    chartGranularity: { seconds: 1_800n, nanos: 0 },
    targetedCount: 2,
    completeCount: 1,
    preliminary: true,
    baselines: [
      create(CohortFirmwareValidationBaselineSchema, {
        previousFirmwareVersion: "1.3.5",
        memberCount: 2,
        eligibleCount: 1,
        state: CohortFirmwareValidationState.AVAILABLE,
        baselineStartTime: historyTimestamp(now - 30 * 60 * 60 * 1000),
        baselineEndTime: historyTimestamp(now - 24 * 60 * 60 * 1000),
        targetStartTime: historyTimestamp(now - 20 * 60 * 60 * 1000),
        targetEndTime: historyTimestamp(now - 14 * 60 * 60 * 1000),
        metrics: [
          metric(MeasurementType.HASHRATE, 100_000_000_000_000, 110_000_000_000_000),
          metric(MeasurementType.EFFICIENCY, 24e-12, 22e-12),
          metric(MeasurementType.POWER, 3_200, 3_250),
        ],
      }),
    ],
  });
};

const buildDevicePage = (cohort: Cohort) => ({
  devices: cohort.members.map((member) =>
    create(CohortDeviceSchema, {
      deviceIdentifier: member.deviceIdentifier,
      display: member.display,
      firmwareStatus: member.firmwareStatus,
      configStatuses: member.configStatuses,
    }),
  ),
  nextPageToken: "",
  totalCount: cohort.members.length,
  availableCount: 0,
  reservedCount: cohort.members.length,
});

const renderPage = () =>
  render(
    <MemoryRouter>
      <CohortOverviewPage />
    </MemoryRouter>,
  );

describe("CohortOverviewPage rollout and validation", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.useParams.mockReturnValue({ cohortId: "7" });
    mocks.routeSiteScope = undefined;
    mocks.duration = "24h";
    mocks.miningPools = [
      { poolId: "1", name: "Primary pool", poolUrl: "stratum+tcp://primary.example", username: "worker" },
      { poolId: "2", name: "Backup one", poolUrl: "stratum+tcp://backup-one.example", username: "worker" },
      { poolId: "3", name: "Backup two", poolUrl: "stratum+tcp://backup-two.example", username: "worker" },
    ];
    mocks.listFirmwareFiles.mockResolvedValue([]);
    mocks.getFirmwareVersionHistory.mockResolvedValue(buildFirmwareHistory());
    mocks.getFirmwareValidation.mockResolvedValue(buildFirmwareValidation());
    mocks.listDevices.mockResolvedValue(buildDevicePage(buildCohort()));
    mocks.addDevices.mockResolvedValue(buildCohort({ deviceIdentifiers: ["miner-001", "miner-002", "eligible-1"] }));
    mocks.listAllDevices.mockResolvedValue([
      create(CohortDeviceSchema, {
        deviceIdentifier: "eligible-1",
        display: create(CohortDeviceDisplaySchema, {
          name: "eligible-1",
          manufacturer: "Proto",
          model: "Rig",
        }),
      }),
      create(CohortDeviceSchema, {
        deviceIdentifier: "eligible-2",
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

  it("renders cohort-specific rollout and validation scoped to explicit members", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort({ firmwareFileId: "fw-1" }));

    renderPage();

    expect(await screen.findByTestId("cohort-rollout-section")).toBeInTheDocument();
    expect(screen.getByText("Rollout status")).toBeInTheDocument();
    expect(screen.getByText("Validation outcomes")).toBeInTheDocument();
    expect(await screen.findByTestId("validation-metric-hashrate")).toBeInTheDocument();
    expect(screen.queryByTestId("uptime-panel")).not.toBeInTheDocument();
    expect(screen.queryByTestId("temperature-panel")).not.toBeInTheDocument();
    expect(screen.getByTestId("validation-metric-power")).toHaveTextContent("Power / miner");
    expect(screen.getByTestId("validation-metric-efficiency")).toHaveTextContent("Efficiency");
    expect(screen.getByText("Preliminary")).toBeInTheDocument();
    expect(await screen.findByTestId("firmware-version-history-panel")).toBeInTheDocument();

    await waitFor(() => expect(mocks.getFirmwareVersionHistory).toHaveBeenCalledTimes(1));
    const historyRequest = mocks.getFirmwareVersionHistory.mock.calls[0]?.[0];
    expect(historyRequest).toEqual(expect.objectContaining({ cohortId: 7n, granularitySeconds: 90 }));
    expect(historyRequest.endTime.getTime() - historyRequest.startTime.getTime()).toBe(24 * 60 * 60 * 1000);

    expect(mocks.getFirmwareValidation).toHaveBeenCalledWith(
      expect.objectContaining({
        cohortId: 7n,
        manufacturer: "Proto",
        model: "Rig",
        comparisonWindow: CohortFirmwareValidationWindow.SIX_HOURS,
      }),
    );
    expect(mocks.useTelemetryMetrics).not.toHaveBeenCalled();
  });

  it("shows miner firmware versions and desired firmware status in the modal", async () => {
    const cohort = buildCohort({ firmwareFileId: "fw-1" });
    mocks.getCohort.mockResolvedValue(cohort);
    mocks.listDevices.mockResolvedValue(buildDevicePage(cohort));
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

    await screen.findByText("Rollout status");
    expect(screen.queryByRole("columnheader", { name: "Miner" })).not.toBeInTheDocument();
    await userEvent.click(screen.getAllByRole("button", { name: "View miners" })[0]);
    expect(await screen.findByRole("columnheader", { name: "Reconciliation status" })).toBeInTheDocument();
    expect(screen.getAllByText("1.3.6").length).toBeGreaterThan(0);
    expect(screen.getAllByText("1.3.5").length).toBeGreaterThan(0);
    expect(screen.getByRole("img", { name: "Firmware: Complete" })).toBeInTheDocument();
    expect(screen.getAllByText("Target: 1.3.6").length).toBeGreaterThan(0);
  });

  it("opens the same miner modal from the header and summary metric without rendering an inline table", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort());
    renderPage();

    await screen.findByText("Rollout status");
    expect(screen.queryByRole("columnheader", { name: "Miner" })).not.toBeInTheDocument();
    const triggers = screen.getAllByRole("button", { name: "View miners" });

    await userEvent.click(triggers[0]);
    expect(await screen.findByTestId("cohort-miners-modal")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(screen.queryByTestId("cohort-miners-modal")).not.toBeInTheDocument();
    expect(screen.getByText("Rollout status")).toBeInTheDocument();

    await userEvent.click(screen.getAllByRole("button", { name: "View miners" })[1]);
    expect(await screen.findByTestId("cohort-miners-modal")).toBeInTheDocument();
  });

  it("keeps pool configuration in the cohort actions menu", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort());
    renderPage();

    await screen.findByText("Rollout status");
    expect(screen.queryByRole("button", { name: "Pools" })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Cohort actions" }));
    const poolsAction = screen.getByTestId("cohort-action-pools");
    expect(poolsAction).toHaveTextContent("Pools");
    expect(poolsAction.querySelector('[data-testid="mining-pools-icon"]')).toBeInTheDocument();
  });

  it("shows the enforced primary and backup pool targets", async () => {
    const cohort = buildCohort({ deviceIdentifiers: ["miner-001"], poolIds: [1n, 2n, 3n] });
    mocks.getCohort.mockResolvedValue(cohort);
    mocks.listDevices.mockResolvedValue(buildDevicePage(cohort));

    renderPage();

    await screen.findByText("Rollout status");
    await userEvent.click(screen.getAllByRole("button", { name: "View miners" })[0]);
    expect(await screen.findByRole("columnheader", { name: "Desired pools" })).toBeInTheDocument();
    expect(screen.getByText("Primary pool")).toBeInTheDocument();
    expect(screen.getByText("Backups: Backup one, Backup two")).toBeInTheDocument();
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

    expect(await screen.findByRole("img", { name: /Firmware rollout: Complete 0, In progress 1/ })).toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });

    await waitFor(() => expect(mocks.getCohort).toHaveBeenCalledTimes(2));
    expect(screen.getByRole("img", { name: /Firmware rollout: Complete 1, In progress 0/ })).toBeInTheDocument();
  });

  it("refreshes cohort details while pool configuration is still applying", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const applying = buildCohort({ deviceIdentifiers: ["miner-001"], poolIds: [1n] });
    applying.summary?.configProgress.push(
      create(CohortConfigProgressSchema, {
        dimension: CohortConfigDimension.POOLS,
        targetedCount: 1,
        applyingCount: 1,
      }),
    );
    const complete = buildCohort({ deviceIdentifiers: ["miner-001"], poolIds: [1n] });
    complete.summary?.configProgress.push(
      create(CohortConfigProgressSchema, {
        dimension: CohortConfigDimension.POOLS,
        targetedCount: 1,
        convergedCount: 1,
      }),
    );
    mocks.getCohort.mockResolvedValueOnce(applying).mockResolvedValue(complete);

    renderPage();

    expect(await screen.findByText("0/1 converged · 1 applying")).toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });

    await waitFor(() => expect(mocks.getCohort).toHaveBeenCalledTimes(2));
    expect(screen.getByText("1/1 converged")).toBeInTheDocument();
  });

  it("groups firmware and pool lifecycle states into cohort rollout segments", async () => {
    const cohort = buildCohort({ deviceIdentifiers: ["miner-001"], firmwareFileId: "fw-1", poolIds: [1n] });
    cohort.summary!.firmwareProgress = create(CohortFirmwareProgressSchema, {
      targetedCount: 6,
      completeCount: 1,
      queuedCount: 1,
      updatingCount: 1,
      verifyingCount: 1,
      needsAttentionCount: 1,
      unknownCount: 1,
    });
    cohort.summary!.configProgress.push(
      create(CohortConfigProgressSchema, {
        dimension: CohortConfigDimension.POOLS,
        targetedCount: 7,
        convergedCount: 1,
        waitingCount: 1,
        applyingCount: 1,
        verifyingCount: 1,
        heldCount: 1,
        failedCount: 1,
        unsupportedCount: 1,
      }),
    );
    mocks.getCohort.mockResolvedValue(cohort);

    renderPage();

    expect(
      await screen.findByRole("img", {
        name: "Firmware rollout: Complete 1, In progress 3, Needs attention 1, Unknown 1",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("img", {
        name: "Pools rollout: Converged 1, In progress 3, Held 1, Failed 1, Unsupported 1",
      }),
    ).toBeInTheDocument();
  });

  it("renders Not enforced for rollout dimensions without desired targets", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort());
    renderPage();

    await screen.findByText("Rollout status");
    expect(screen.getAllByText("Not enforced")).toHaveLength(2);
  });

  it("keeps historical graphs hidden for the default cohort", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort({ isDefault: true }));

    renderPage();

    await screen.findByText("Default cohort");
    expect(screen.queryByTestId("cohort-rollout-section")).not.toBeInTheDocument();
    expect(mocks.useTelemetryMetrics).not.toHaveBeenCalled();
    expect(mocks.getFirmwareValidation).not.toHaveBeenCalled();
    expect(mocks.getFirmwareVersionHistory).not.toHaveBeenCalled();
  });

  it("shows an add-miners prompt and does not request validation for empty active cohorts", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort({ deviceIdentifiers: [] }));

    renderPage();

    expect(await screen.findByTestId("cohort-empty-rollout-state")).toHaveTextContent(
      "Add miners to begin cohort validation",
    );
    expect(mocks.useTelemetryMetrics).not.toHaveBeenCalled();
    expect(mocks.getFirmwareValidation).not.toHaveBeenCalled();
    expect(screen.queryByTestId("firmware-version-history-panel")).not.toBeInTheDocument();
    expect(mocks.getFirmwareVersionHistory).not.toHaveBeenCalled();
  });

  it("shows a non-actionable empty state for released cohorts", async () => {
    const cohort = buildCohort({ deviceIdentifiers: [] });
    cohort.summary!.state = CohortState.RELEASED;
    mocks.getCohort.mockResolvedValue(cohort);

    renderPage();

    expect(await screen.findByTestId("cohort-empty-rollout-state")).toHaveTextContent(
      "No miners remain in this cohort",
    );
    expect(screen.queryByRole("button", { name: "Add miners" })).not.toBeInTheDocument();
  });

  it("does not apply route site scope to cohort firmware validation", async () => {
    mocks.routeSiteScope = { kind: "site", id: "42", label: "Austin" };
    mocks.getCohort.mockResolvedValue(buildCohort({ firmwareFileId: "fw-1" }));

    renderPage();

    await screen.findByTestId("cohort-rollout-section");
    await waitFor(() => expect(mocks.getFirmwareValidation).toHaveBeenCalled());
    const validationRequest =
      mocks.getFirmwareValidation.mock.calls[mocks.getFirmwareValidation.mock.calls.length - 1]?.[0];
    expect(validationRequest).toEqual(
      expect.objectContaining({
        cohortId: 7n,
        manufacturer: "Proto",
        model: "Rig",
      }),
    );
    expect(validationRequest).not.toHaveProperty("siteIds");
    expect(validationRequest).not.toHaveProperty("includeUnassigned");
  });

  it("keeps firmware history duration separate from the validation window", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort({ firmwareFileId: "fw-1" }));
    const { rerender } = renderPage();

    await screen.findByTestId("cohort-rollout-section");
    await waitFor(() => expect(mocks.getFirmwareValidation).toHaveBeenCalled());
    expect(mocks.getFirmwareValidation.mock.calls[mocks.getFirmwareValidation.mock.calls.length - 1]?.[0]).toEqual(
      expect.objectContaining({ comparisonWindow: CohortFirmwareValidationWindow.SIX_HOURS }),
    );

    mocks.duration = "7d";
    rerender(
      <MemoryRouter>
        <CohortOverviewPage />
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(
        mocks.getFirmwareVersionHistory.mock.calls[mocks.getFirmwareVersionHistory.mock.calls.length - 1]?.[0],
      ).toEqual(expect.objectContaining({ cohortId: 7n, granularitySeconds: 900 })),
    );
    expect(mocks.getFirmwareValidation.mock.calls[mocks.getFirmwareValidation.mock.calls.length - 1]?.[0]).toEqual(
      expect.objectContaining({ comparisonWindow: CohortFirmwareValidationWindow.SIX_HOURS }),
    );
  });

  it("allows the duration selector to update the fleet duration", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort());
    renderPage();

    await screen.findByTestId("cohort-rollout-section");
    await userEvent.click(screen.getByRole("button", { name: "7d" }));

    expect(mocks.setDuration).toHaveBeenCalledWith("7d");
  });

  it("shows a local validation error without replacing the cohort page", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort({ firmwareFileId: "fw-1" }));
    mocks.getFirmwareValidation.mockRejectedValue(new Error("failed"));

    renderPage();

    expect(await screen.findByTestId("cohort-rollout-section")).toBeInTheDocument();
    expect(await screen.findByText("Couldn't load firmware validation")).toBeInTheDocument();
    expect(screen.getByText("Miners")).toBeInTheDocument();
  });

  it("shows a local firmware history error without replacing validation or summary", async () => {
    mocks.getCohort.mockResolvedValue(buildCohort({ firmwareFileId: "fw-1" }));
    mocks.getFirmwareVersionHistory.mockRejectedValue(new Error("failed"));

    renderPage();

    expect(await screen.findByText("Couldn't load firmware history")).toBeInTheDocument();
    expect(await screen.findByTestId("validation-metric-hashrate")).toBeInTheDocument();
    expect(screen.getByText("Miners")).toBeInTheDocument();
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
