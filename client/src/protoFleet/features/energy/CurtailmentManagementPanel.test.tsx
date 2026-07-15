import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";

import type { UseCurtailmentApiResult } from "@/protoFleet/api/useCurtailmentApi";
import type { ActiveCurtailmentEvent } from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import type { CurtailmentHistoryEvent } from "@/protoFleet/features/energy/CurtailmentHistory";
import CurtailmentManagementPanel from "@/protoFleet/features/energy/CurtailmentManagementPanel";
import type {
  CurtailmentPlanPreview,
  CurtailmentResponseProfileOption,
  CurtailmentSiteOption,
  CurtailmentSubmitValues,
} from "@/protoFleet/features/energy/CurtailmentStartModal";

const mocks = vi.hoisted(() => ({
  activeSite: { current: { kind: "all" } as { kind: string; id?: string; slug?: string } },
  adminTerminateCurtailment: vi.fn(),
  dismissTerminalCurtailment: vi.fn(),
  forceReleaseCurtailment: vi.fn(),
  goToHistoryPage: vi.fn(),
  listSites: vi.fn(),
  listInfrastructureDevices: vi.fn(),
  navigate: vi.fn(),
  refreshCurtailment: vi.fn(),
  selectActiveCurtailment: vi.fn(),
  setHistoryStatusFilter: vi.fn(),
  setHistoryStatusFilters: vi.fn(),
  startCurtailment: vi.fn(),
  stopCurtailment: vi.fn(),
  submitValues: { reason: "Grid peak" },
  updateCurtailment: vi.fn(),
  useHasPermission: vi.fn(),
  useCurtailmentApi: vi.fn(),
  useCurtailmentResponseProfiles: vi.fn(),
  useInfrastructureDevices: vi.fn(),
}));

vi.mock("@/protoFleet/api/useCurtailmentApi", () => ({
  adminTerminateReasonRequiredMessage: "Enter a reason before terminating the event.",
  useCurtailmentApi: (options?: unknown) => mocks.useCurtailmentApi(options),
}));

vi.mock("@/protoFleet/api/useCurtailmentResponseProfiles", () => ({
  default: (...args: unknown[]) => mocks.useCurtailmentResponseProfiles(...args),
}));

vi.mock("@/protoFleet/api/useInfrastructureDevices", () => ({
  default: (...args: unknown[]) => mocks.useInfrastructureDevices(...args),
}));

vi.mock("@/protoFleet/api/sites", () => ({
  useSites: () => ({
    listSites: mocks.listSites,
  }),
}));

vi.mock("@/protoFleet/components/PageHeader/SitePicker", () => ({
  useActiveSite: () => ({ activeSite: mocks.activeSite.current, setActiveSite: vi.fn() }),
}));

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: (key: string) => mocks.useHasPermission(key),
}));

vi.mock("react-router-dom", () => ({
  useNavigate: () => mocks.navigate,
}));

vi.mock("@/protoFleet/features/energy/ActiveCurtailmentStatus", () => ({
  default: ({
    onDismissRestored,
    onRequestEdit,
    onRequestForceRelease,
    onRequestRestore,
    onRequestStop,
    onRequestTerminateRecovery,
  }: {
    onDismissRestored?: () => void;
    onRequestEdit?: () => void;
    onRequestForceRelease?: () => void;
    onRequestRestore?: () => void;
    onRequestStop?: () => void;
    onRequestTerminateRecovery?: () => void;
  }) => (
    <div data-testid="active-curtailment-status">
      <button type="button" onClick={onDismissRestored}>
        Dismiss restored
      </button>
      {onRequestEdit ? (
        <button type="button" onClick={onRequestEdit}>
          Request edit
        </button>
      ) : null}
      {onRequestForceRelease ? (
        <button type="button" onClick={onRequestForceRelease}>
          Request abort
        </button>
      ) : null}
      {onRequestRestore ? (
        <button type="button" onClick={onRequestRestore}>
          Request restore
        </button>
      ) : null}
      {onRequestStop ? (
        <button type="button" onClick={onRequestStop}>
          Request restore now
        </button>
      ) : null}
      {onRequestTerminateRecovery ? (
        <button type="button" onClick={onRequestTerminateRecovery}>
          Request stop restore
        </button>
      ) : null}
    </div>
  ),
}));

vi.mock("@/protoFleet/features/energy/CurtailmentHistory", () => ({
  default: ({
    currentPage,
    events,
    hasNextPage,
    hasPreviousPage,
    pageSize,
    selectedStatusFilters,
    onPageChange,
    onManageActiveEvent,
    onSelectActiveEvent,
    onStatusFiltersChange,
    onStopActiveEvent,
    onStopActiveEventRequested,
  }: {
    currentPage?: number;
    events: CurtailmentHistoryEvent[];
    hasNextPage?: boolean;
    hasPreviousPage?: boolean;
    pageSize?: number;
    selectedStatusFilters?: string[];
    onPageChange?: (page: number) => void;
    onManageActiveEvent?: (event: CurtailmentHistoryEvent) => void;
    onSelectActiveEvent?: (event: CurtailmentHistoryEvent) => void;
    onStatusFiltersChange?: (filters: string[]) => void;
    onStopActiveEvent?: (event: CurtailmentHistoryEvent) => void | Promise<unknown>;
    onStopActiveEventRequested?: (event: CurtailmentHistoryEvent) => void;
  }) => (
    <div data-testid="curtailment-history">
      <div data-testid="history-page">{currentPage}</div>
      <div data-testid="history-page-size">{pageSize}</div>
      <div data-testid="history-has-next">{String(hasNextPage)}</div>
      <div data-testid="history-has-previous">{String(hasPreviousPage)}</div>
      <div data-testid="history-status-filter">{selectedStatusFilters?.join(",") ?? ""}</div>
      <div data-testid="history-events">{events.map((event) => event.id).join(",")}</div>
      <button type="button" onClick={() => onPageChange?.(2)}>
        Load page 2
      </button>
      <button type="button" onClick={() => onStatusFiltersChange?.(["completed", "failed"])}>
        Filter completed and failed
      </button>
      {onManageActiveEvent ? (
        <>
          <button
            type="button"
            disabled={events.length === 0}
            onClick={() => {
              if (events[0]) {
                onManageActiveEvent(events[0]);
              }
            }}
          >
            Manage history event
          </button>
          {events[1] ? (
            <button type="button" onClick={() => onManageActiveEvent(events[1])}>
              Manage second history event
            </button>
          ) : null}
        </>
      ) : null}
      {onSelectActiveEvent && events[0] ? (
        <button type="button" onClick={() => onSelectActiveEvent(events[0])}>
          Select history event
        </button>
      ) : null}
      {onStopActiveEvent ? (
        <button
          type="button"
          disabled={events.length === 0}
          onClick={() => {
            if (events[0]) {
              onStopActiveEventRequested?.(events[0]);
              onStopActiveEvent(events[0]);
            }
          }}
        >
          Stop history event
        </button>
      ) : null}
    </div>
  ),
}));

vi.mock("@/protoFleet/features/energy/CurtailmentStartModal", () => ({
  getDefaultCurtailmentSiteScope: (
    activeSite: { kind: string; id?: string },
    siteOptions: CurtailmentSiteOption[] = [],
  ) => {
    if (activeSite.kind !== "site" || !activeSite.id) {
      return undefined;
    }

    return (
      siteOptions.find((siteOption) => siteOption.id === activeSite.id) ?? {
        id: activeSite.id,
        name: `Site ${activeSite.id}`,
      }
    );
  },
  default: ({
    initialValues,
    mode,
    onStopCurtailment,
    onSubmit,
    preview,
    responseProfiles,
    siteOptions,
    defaultSiteScope,
    infrastructureDevices,
  }: {
    initialValues?: Partial<CurtailmentSubmitValues>;
    mode?: string;
    onStopCurtailment?: () => void;
    onSubmit: (values: CurtailmentSubmitValues) => void;
    preview?: CurtailmentPlanPreview;
    responseProfiles?: CurtailmentResponseProfileOption[];
    siteOptions?: CurtailmentSiteOption[];
    defaultSiteScope?: CurtailmentSiteOption;
    infrastructureDevices?: { name: string }[];
  }) => (
    <div role="dialog" aria-label={mode === "edit" ? "Manage curtailment" : "New curtailment"}>
      <div data-testid="modal-initial-reason">{initialValues?.reason ?? ""}</div>
      <div data-testid="modal-response-profiles">{responseProfiles?.map((profile) => profile.label).join(",")}</div>
      <div data-testid="modal-response-profile-values">{JSON.stringify(responseProfiles?.[0]?.values ?? {})}</div>
      <div data-testid="modal-site-options">{siteOptions?.map((siteOption) => siteOption.name).join(",")}</div>
      <div data-testid="modal-default-site-scope">{defaultSiteScope?.name ?? ""}</div>
      <div data-testid="modal-infrastructure-devices">
        {infrastructureDevices?.map((device) => device.name).join(",")}
      </div>
      <div data-testid="modal-preview">
        {preview
          ? `${preview.selectedMinerCount} miners, ${preview.targetKw} kW target, ${preview.estimatedReductionKw} kW estimated`
          : ""}
      </div>
      <button type="button" onClick={() => onSubmit(mocks.submitValues as CurtailmentSubmitValues)}>
        Submit {mode === "edit" ? "edit" : "plan"}
      </button>
      {mode === "edit" ? (
        <button type="button" onClick={onStopCurtailment}>
          Stop from editor
        </button>
      ) : null}
    </div>
  ),
}));

vi.mock("@/protoFleet/features/energy/CurtailmentStopConfirmationDialog", () => ({
  default: ({ action, onConfirm }: { action: string; onConfirm: () => void }) => (
    <div role="dialog" aria-label={`${action} confirmation`}>
      <button type="button" onClick={onConfirm}>
        Confirm confirmation
      </button>
    </div>
  ),
}));

const activeEvent = {
  reason: "Grid peak",
  state: "active",
  scopeLabel: "Whole fleet",
  sourceLabel: "Manual",
  isAutomationOwned: false,
  selectedMiners: 2,
  targetKw: 5,
  estimatedReductionKw: 6.2,
} as ActiveCurtailmentEvent;
const activeEventFormValues = {
  reason: "Grid peak",
  scopeType: "wholeOrg",
  scopeId: "whole-org",
  deviceSetIds: [],
  deviceIdentifiers: [],
  responseProfileId: "customPlan",
  curtailmentMode: "fixedKwReduction",
  minerSelectionStrategy: "leastEfficientFirst",
  targetKw: "5",
  toleranceKw: "",
  priority: "normal",
  minDurationSec: "60",
  maxDurationSec: "300",
  curtailBatchSize: "",
  curtailBatchIntervalSec: "",
  restoreBatchSize: "1",
  restoreIntervalSec: "60",
  includeMaintenance: true,
  forceIncludeAllPairedMiners: false,
} satisfies CurtailmentSubmitValues;
const historyEvent = { id: "curt-1" } as CurtailmentHistoryEvent;

const emptySnapshot = {
  activeEvent: null,
  activeEvents: [],
  activeEventId: null,
  activeEventFormValues: null,
  historyEvents: [],
};

function createApiResult(overrides: Partial<UseCurtailmentApiResult> = {}): UseCurtailmentApiResult {
  return {
    activeEvent: null,
    activeEvents: [],
    activeEventId: null,
    historyEvents: [],
    activeEventFormValues: null,
    isLoading: false,
    isStarting: false,
    isUpdating: false,
    stoppingEventId: null,
    adminTerminatingEventId: null,
    loadError: null,
    startError: null,
    updateError: null,
    stopError: null,
    adminTerminateError: null,
    historyCurrentPage: 0,
    historyHasNextPage: false,
    historyHasPreviousPage: false,
    historyPageSize: 50,
    historyStatusFilters: [],
    refreshCurtailment: mocks.refreshCurtailment as UseCurtailmentApiResult["refreshCurtailment"],
    goToHistoryPage: mocks.goToHistoryPage as UseCurtailmentApiResult["goToHistoryPage"],
    setHistoryStatusFilter: mocks.setHistoryStatusFilter as UseCurtailmentApiResult["setHistoryStatusFilter"],
    setHistoryStatusFilters: mocks.setHistoryStatusFilters as UseCurtailmentApiResult["setHistoryStatusFilters"],
    selectActiveCurtailment: mocks.selectActiveCurtailment as UseCurtailmentApiResult["selectActiveCurtailment"],
    startCurtailment: mocks.startCurtailment as UseCurtailmentApiResult["startCurtailment"],
    dismissTerminalCurtailment:
      mocks.dismissTerminalCurtailment as UseCurtailmentApiResult["dismissTerminalCurtailment"],
    updateCurtailment: mocks.updateCurtailment as UseCurtailmentApiResult["updateCurtailment"],
    stopCurtailment: mocks.stopCurtailment as UseCurtailmentApiResult["stopCurtailment"],
    adminTerminateCurtailment: mocks.adminTerminateCurtailment as UseCurtailmentApiResult["adminTerminateCurtailment"],
    forceReleaseCurtailment: mocks.forceReleaseCurtailment as UseCurtailmentApiResult["forceReleaseCurtailment"],
    ...overrides,
  };
}

describe("CurtailmentManagementPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.activeSite.current = { kind: "all" };
    mocks.listSites.mockImplementation(({ onSuccess, onFinally } = {}) => {
      onSuccess?.([{ site: { id: 101n, name: "Austin, TX" } }]);
      onFinally?.();
    });
    mocks.refreshCurtailment.mockResolvedValue(emptySnapshot);
    mocks.goToHistoryPage.mockResolvedValue(emptySnapshot);
    mocks.selectActiveCurtailment.mockResolvedValue({
      activeEvent: null,
      activeEventId: null,
      activeEventFormValues: null,
    });
    mocks.setHistoryStatusFilter.mockResolvedValue(emptySnapshot);
    mocks.setHistoryStatusFilters.mockResolvedValue(emptySnapshot);
    mocks.startCurtailment.mockResolvedValue({});
    mocks.stopCurtailment.mockResolvedValue({});
    mocks.adminTerminateCurtailment.mockResolvedValue({});
    mocks.forceReleaseCurtailment.mockResolvedValue({});
    mocks.updateCurtailment.mockResolvedValue({});
    mocks.useHasPermission.mockReturnValue(false);
    mocks.useCurtailmentApi.mockReturnValue(createApiResult());
    mocks.useCurtailmentResponseProfiles.mockReturnValue({
      responseProfiles: [],
      isLoading: false,
      isCreating: false,
      updatingProfileIds: new Set(),
      loadError: null,
      createError: null,
      listResponseProfiles: vi.fn(),
      createResponseProfile: vi.fn(),
      updateResponseProfile: vi.fn(),
      deleteResponseProfile: vi.fn(),
    });
    mocks.useInfrastructureDevices.mockReturnValue({
      devices: [],
      isLoading: false,
      loadError: null,
      updatingDeviceIds: new Set(),
      listDevices: mocks.listInfrastructureDevices,
      createDevice: vi.fn(),
      updateDevice: vi.fn(),
      setDeviceEnabled: vi.fn(),
      deleteDevice: vi.fn(),
    });
  });

  it("loads site names and passes them to curtailment hooks when the operator can read sites", async () => {
    const user = userEvent.setup();
    mocks.useHasPermission.mockImplementation((key: string) => key === "site:read");

    render(<CurtailmentManagementPanel />);

    await waitFor(() => expect(mocks.listSites).toHaveBeenCalled());
    await waitFor(() => {
      const latestApiCall = mocks.useCurtailmentApi.mock.calls[mocks.useCurtailmentApi.mock.calls.length - 1];
      const latestResponseProfileCall =
        mocks.useCurtailmentResponseProfiles.mock.calls[mocks.useCurtailmentResponseProfiles.mock.calls.length - 1];
      const apiOptions = latestApiCall?.[0] as { siteNameById?: Map<string, string> } | undefined;
      const responseProfileOptions = latestResponseProfileCall?.[1] as
        { siteNameById?: Map<string, string> } | undefined;

      expect(apiOptions?.siteNameById?.get("101")).toBe("Austin, TX");
      expect(responseProfileOptions?.siteNameById?.get("101")).toBe("Austin, TX");
    });

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));

    expect(screen.getByTestId("modal-site-options")).toHaveTextContent("Austin, TX");
  });

  it("passes the globally selected site as the default site scope for new curtailment runs", async () => {
    const user = userEvent.setup();
    mocks.activeSite.current = { kind: "site", id: "101", slug: "austin" };
    mocks.useHasPermission.mockImplementation((key: string) => key === "site:read");

    render(<CurtailmentManagementPanel />);

    await waitFor(() => expect(mocks.listSites).toHaveBeenCalled());
    await user.click(screen.getByRole("button", { name: "Run curtailment" }));

    expect(screen.getByTestId("modal-default-site-scope")).toHaveTextContent("Austin, TX");
  });

  it("populates the run curtailment modal with infrastructure devices from the shared hook", async () => {
    const user = userEvent.setup();
    mocks.useHasPermission.mockImplementation((key: string) => key === "site:read");
    mocks.useInfrastructureDevices.mockReturnValue({
      devices: [
        {
          id: "31",
          siteId: "101",
          siteName: "Austin, TX",
          buildingName: "Building 1",
          name: "Fan Unit 1",
          deviceKind: "single_fan",
          fanCount: 1,
          enabled: true,
          driverType: "modbus",
          driverConfig: "",
        },
      ],
      isLoading: false,
      loadError: null,
      updatingDeviceIds: new Set(),
      listDevices: mocks.listInfrastructureDevices,
      createDevice: vi.fn(),
      updateDevice: vi.fn(),
      setDeviceEnabled: vi.fn(),
      deleteDevice: vi.fn(),
    });

    render(<CurtailmentManagementPanel />);

    expect(mocks.useInfrastructureDevices).toHaveBeenCalledWith(true);
    await user.click(screen.getByRole("button", { name: "Run curtailment" }));

    expect(screen.getByTestId("modal-infrastructure-devices")).toHaveTextContent("Fan Unit 1");
  });

  it("submits planned curtailments, closes the modal, and passes refreshed history props through", async () => {
    const user = userEvent.setup();
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        historyCurrentPage: 1,
        historyEvents: [historyEvent],
        historyHasPreviousPage: true,
      }),
    );

    const { rerender } = render(<CurtailmentManagementPanel />);

    expect(screen.getByTestId("history-page")).toHaveTextContent("1");

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));
    await user.click(screen.getByRole("button", { name: "Submit plan" }));

    await waitFor(() => expect(mocks.startCurtailment).toHaveBeenCalledWith(mocks.submitValues));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "New curtailment" })).not.toBeInTheDocument());

    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        historyCurrentPage: 0,
        historyEvents: [{ ...historyEvent, id: "curt-2" }],
        historyHasNextPage: true,
      }),
    );
    rerender(<CurtailmentManagementPanel />);

    expect(screen.getByTestId("history-page")).toHaveTextContent("0");
    expect(screen.getByTestId("history-has-next")).toHaveTextContent("true");
    expect(screen.getByTestId("history-events")).toHaveTextContent("curt-2");
  });

  it("navigates to curtailment settings from the secondary CTA", async () => {
    const user = userEvent.setup();

    render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Edit settings" }));

    expect(mocks.navigate).toHaveBeenCalledWith("/settings/curtailment");
    expect(screen.queryByRole("dialog", { name: "New curtailment" })).not.toBeInTheDocument();
  });

  it("passes response profiles to the plan modal", async () => {
    const user = userEvent.setup();
    mocks.useCurtailmentResponseProfiles.mockReturnValue({
      responseProfiles: [
        {
          id: "profile-1",
          name: "Standard shed",
          targetSummary: "50 kW target",
          scope: "Whole fleet",
          selectionStrategy: "Least efficient first",
          restoreBehavior: "Restore in batches",
          deadlineSummary: "Within 15 min",
          formValues: {
            name: "Standard shed",
            actionType: "fixedKwReduction",
            targetKw: "50",
            deviceIdentifiers: ["miner-1", "miner-2", "miner-3"],
            siteId: "101",
            siteName: "Austin, TX",
            selectionStrategy: "leastEfficientFirst",
            restoreBehavior: "automaticBatchRestore",
            minDurationSec: "300",
            maxDurationSec: "900",
            curtailBatchSize: "20",
            curtailBatchIntervalSec: "60",
            restoreBatchSize: "10",
            restoreIntervalSec: "120",
            responseDeadlineMinutes: "15",
            includeMaintenance: true,
          },
        },
      ],
      isLoading: false,
      isCreating: false,
      updatingProfileIds: new Set(),
      loadError: null,
      createError: null,
      listResponseProfiles: vi.fn(),
      createResponseProfile: vi.fn(),
      updateResponseProfile: vi.fn(),
      deleteResponseProfile: vi.fn(),
    });

    render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));

    expect(screen.getByTestId("modal-response-profiles")).toHaveTextContent("Standard shed");
    expect(screen.getByTestId("modal-response-profile-values")).toHaveTextContent('"scopeType":"explicitMiners"');
    expect(screen.getByTestId("modal-response-profile-values")).toHaveTextContent('"scopeId":"Austin, TX"');
    expect(screen.getByTestId("modal-response-profile-values")).toHaveTextContent('"siteSelection":"site"');
    expect(screen.getByTestId("modal-response-profile-values")).toHaveTextContent('"siteId":"101"');
    expect(screen.getByTestId("modal-response-profile-values")).toHaveTextContent(
      '"deviceIdentifiers":["miner-1","miner-2","miner-3"]',
    );
    expect(screen.getByTestId("modal-response-profile-values")).toHaveTextContent('"targetKw":"50"');
  });

  it("passes miner-scoped response profiles to the plan modal", async () => {
    const user = userEvent.setup();
    mocks.useCurtailmentResponseProfiles.mockReturnValue({
      responseProfiles: [
        {
          id: "profile-1",
          name: "Targeted shed",
          targetSummary: "50 kW target",
          scope: "3 miners",
          selectionStrategy: "Least efficient first",
          restoreBehavior: "Restore in batches",
          deadlineSummary: "Within 15 min",
          formValues: {
            name: "Targeted shed",
            actionType: "fixedKwReduction",
            targetKw: "50",
            deviceIdentifiers: ["miner-1", "miner-2", "miner-3"],
            siteId: "",
            siteName: "",
            selectionStrategy: "leastEfficientFirst",
            restoreBehavior: "automaticBatchRestore",
            minDurationSec: "300",
            maxDurationSec: "900",
            curtailBatchSize: "20",
            curtailBatchIntervalSec: "60",
            restoreBatchSize: "10",
            restoreIntervalSec: "120",
            responseDeadlineMinutes: "15",
            includeMaintenance: true,
          },
        },
      ],
      isLoading: false,
      isCreating: false,
      updatingProfileIds: new Set(),
      loadError: null,
      createError: null,
      listResponseProfiles: vi.fn(),
      createResponseProfile: vi.fn(),
      updateResponseProfile: vi.fn(),
      deleteResponseProfile: vi.fn(),
    });

    render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));

    expect(screen.getByTestId("modal-response-profiles")).toHaveTextContent("Targeted shed");
    expect(screen.getByTestId("modal-response-profile-values")).toHaveTextContent('"scopeType":"explicitMiners"');
    expect(screen.getByTestId("modal-response-profile-values")).toHaveTextContent(
      '"deviceIdentifiers":["miner-1","miner-2","miner-3"]',
    );
    expect(screen.getByTestId("modal-response-profile-values")).toHaveTextContent('"siteId":""');
  });

  it("opens a new plan while a curtailment is already active", async () => {
    const user = userEvent.setup();
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
        activeEventId: "curt-1",
        activeEventFormValues,
      }),
    );

    render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Run curtailment" }));

    expect(screen.getByRole("dialog", { name: "New curtailment" })).toBeInTheDocument();
    expect(screen.queryByTestId("active-curtailment-limit-dialog")).not.toBeInTheDocument();
    expect(mocks.startCurtailment).not.toHaveBeenCalled();
  });

  it("calls stop curtailment from restore, stop, and history requests", async () => {
    const user = userEvent.setup();
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
        activeEvents: [{ ...historyEvent, state: "active" }],
        activeEventId: "curt-1",
        historyEvents: [historyEvent],
      }),
    );

    render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Request restore" }));
    expect(screen.getByRole("dialog", { name: "restore confirmation" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Confirm confirmation" }));
    await waitFor(() => expect(mocks.stopCurtailment).toHaveBeenCalledWith("curt-1"));

    await waitFor(() => expect(screen.queryByRole("dialog", { name: "restore confirmation" })).not.toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: "Request restore now" }));
    expect(screen.getByRole("dialog", { name: "restore confirmation" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Confirm confirmation" }));
    await waitFor(() => expect(mocks.stopCurtailment).toHaveBeenCalledTimes(2));

    await user.click(screen.getByRole("button", { name: "Stop history event" }));

    expect(mocks.stopCurtailment).toHaveBeenLastCalledWith("curt-1");
  });

  it("shows restore for automation-owned events", () => {
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, isAutomationOwned: true },
        activeEvents: [{ ...historyEvent, state: "active" }],
        activeEventId: "curt-1",
      }),
    );

    render(<CurtailmentManagementPanel enableRecover />);

    expect(screen.getByRole("button", { name: "Request restore" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request restore now" })).not.toBeInTheDocument();
  });

  it("aborts restoring curtailment ownership for admin recovery users", async () => {
    const user = userEvent.setup();
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, state: "restoring" },
        activeEvents: [{ ...historyEvent, state: "restoring" }],
        activeEventId: "curt-1",
      }),
    );
    mocks.useHasPermission.mockImplementation((permission: string) =>
      ["curtailment:manage", "curtailment:read", "site:read", "admin:recovery"].includes(permission),
    );

    render(<CurtailmentManagementPanel enableManage enableRecover />);

    await user.click(screen.getByRole("button", { name: "Request abort" }));
    expect(screen.getByText("Abort restore?")).toBeInTheDocument();
    expect(screen.getByText(/aborts the restore workflow/i)).toBeInTheDocument();

    const releaseButtons = screen.getAllByRole("button", { name: "Abort restore" });
    await user.click(releaseButtons[releaseButtons.length - 1]);
    expect(screen.getByText("Enter a reason before terminating the event.")).toBeInTheDocument();

    await user.type(screen.getByRole("textbox", { name: "Reason" }), "Operator needs manual control");
    const updatedReleaseButtons = screen.getAllByRole("button", { name: "Abort restore" });
    await user.click(updatedReleaseButtons[updatedReleaseButtons.length - 1]);

    await waitFor(() =>
      expect(mocks.forceReleaseCurtailment).toHaveBeenCalledWith("curt-1", {
        reason: "Operator needs manual control",
      }),
    );
  });

  it("hides abort while non-automation curtailment is still pending or active", () => {
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
        activeEvents: [{ ...historyEvent, state: "active" }],
        activeEventId: "curt-1",
      }),
    );
    mocks.useHasPermission.mockImplementation((permission: string) =>
      ["curtailment:manage", "curtailment:read", "site:read", "admin:recovery"].includes(permission),
    );

    render(<CurtailmentManagementPanel enableManage enableRecover />);

    expect(screen.queryByRole("button", { name: "Request abort" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Request restore now" })).toBeInTheDocument();
  });

  it("shows abort for automation-owned active curtailments", async () => {
    const user = userEvent.setup();
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, isAutomationOwned: true },
        activeEvents: [{ ...historyEvent, state: "active" }],
        activeEventId: "curt-1",
      }),
    );
    mocks.useHasPermission.mockImplementation((permission: string) =>
      ["curtailment:manage", "curtailment:read", "site:read", "admin:recovery"].includes(permission),
    );

    render(<CurtailmentManagementPanel enableManage enableRecover />);

    expect(screen.queryByRole("button", { name: "Request restore now" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Request restore" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Request abort" }));
    expect(screen.getByText("Abort curtailment?")).toBeInTheDocument();
    expect(screen.getByText(/disables the owning automation rule/i)).toBeInTheDocument();

    await user.type(screen.getByRole("textbox", { name: "Reason" }), "Need to disable automation");
    const abortButtons = screen.getAllByRole("button", { name: "Abort curtailment" });
    await user.click(abortButtons[abortButtons.length - 1]);

    await waitFor(() =>
      expect(mocks.forceReleaseCurtailment).toHaveBeenCalledWith("curt-1", {
        reason: "Need to disable automation",
      }),
    );
  });

  it("hides force recovery controls from non-admin curtailment managers", () => {
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, isAutomationOwned: true },
        activeEventId: "curt-1",
      }),
    );

    render(<CurtailmentManagementPanel enableRecover={false} />);

    expect(screen.queryByRole("button", { name: "Request stop restore" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request abort" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Request restore" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request restore now" })).not.toBeInTheDocument();
  });

  it("does not offer abort for restored curtailments", () => {
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, isAutomationOwned: true, state: "completed" },
        activeEvents: [{ ...historyEvent, state: "completed" }],
        activeEventId: "curt-1",
      }),
    );

    render(<CurtailmentManagementPanel enableManage enableRecover />);

    expect(screen.getByRole("button", { name: "Dismiss restored" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request abort" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request stop restore" })).not.toBeInTheDocument();
  });

  it("shows stop restore before abort restore for automation restoring events", async () => {
    const user = userEvent.setup();
    mocks.adminTerminateCurtailment.mockRejectedValueOnce(new Error("terminate failed"));
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, isAutomationOwned: true, state: "restoring" },
        activeEvents: [{ ...historyEvent, state: "restoring" }],
        activeEventId: "curt-1",
      }),
    );

    render(<CurtailmentManagementPanel enableRecover />);

    expect(screen.getByRole("button", { name: "Request stop restore" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request abort" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Request stop restore" }));
    await user.type(screen.getByRole("textbox", { name: "Reason" }), "Recovery cannot complete");
    await user.click(screen.getByRole("button", { name: "Stop restore" }));

    await waitFor(() =>
      expect(mocks.adminTerminateCurtailment).toHaveBeenCalledWith("curt-1", {
        reason: "Recovery cannot complete",
        targetState: "cancelled",
      }),
    );

    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.queryByRole("button", { name: "Request stop restore" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Request abort" })).toBeInTheDocument();
  });

  it("requires a fresh stop restore failure to re-offer abort in a new restore cycle", async () => {
    const user = userEvent.setup();
    mocks.adminTerminateCurtailment.mockRejectedValueOnce(new Error("terminate failed"));
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, isAutomationOwned: true, state: "restoring" },
        activeEvents: [{ ...historyEvent, state: "restoring" }],
        activeEventId: "curt-1",
      }),
    );

    const { rerender } = render(<CurtailmentManagementPanel enableRecover />);

    await user.click(screen.getByRole("button", { name: "Request stop restore" }));
    await user.type(screen.getByRole("textbox", { name: "Reason" }), "Recovery cannot complete");
    await user.click(screen.getByRole("button", { name: "Stop restore" }));
    await waitFor(() => expect(mocks.adminTerminateCurtailment).toHaveBeenCalled());
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.getByRole("button", { name: "Request abort" })).toBeInTheDocument();

    // Recurtail cycle: the same event leaves restoring and later re-enters it.
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, isAutomationOwned: true, state: "active" },
        activeEvents: [{ ...historyEvent, state: "active" }],
        activeEventId: "curt-1",
      }),
    );
    rerender(<CurtailmentManagementPanel enableRecover />);

    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, isAutomationOwned: true, state: "restoring" },
        activeEvents: [{ ...historyEvent, state: "restoring" }],
        activeEventId: "curt-1",
      }),
    );
    rerender(<CurtailmentManagementPanel enableRecover />);

    expect(screen.getByRole("button", { name: "Request stop restore" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request abort" })).not.toBeInTheDocument();
  });

  it("keeps stop restore available after terminate recovery validation failures", async () => {
    const user = userEvent.setup();
    mocks.adminTerminateCurtailment.mockRejectedValueOnce(new Error("Enter a reason before terminating the event."));
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, isAutomationOwned: true, state: "restoring" },
        activeEvents: [{ ...historyEvent, state: "restoring" }],
        activeEventId: "curt-1",
      }),
    );

    render(<CurtailmentManagementPanel enableRecover />);

    await user.click(screen.getByRole("button", { name: "Request stop restore" }));
    await user.type(screen.getByRole("textbox", { name: "Reason" }), "Recovery cannot complete");
    await user.click(screen.getByRole("button", { name: "Stop restore" }));

    await waitFor(() => expect(mocks.adminTerminateCurtailment).toHaveBeenCalled());
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.getByRole("button", { name: "Request stop restore" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request abort" })).not.toBeInTheDocument();
  });

  it("keeps stop restore available after transient terminate recovery failures", async () => {
    const user = userEvent.setup();
    // Match the production error shape: useCurtailmentApi's handleFailure/toError
    // wraps the ConnectError in a plain Error carrying it as `cause`, so this
    // exercises the cause-unwrapping branch of the panel's transient detection.
    mocks.adminTerminateCurtailment.mockRejectedValueOnce(
      Object.assign(new Error("temporarily unavailable"), {
        cause: new ConnectError("temporarily unavailable", Code.Unavailable),
      }),
    );
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, isAutomationOwned: true, state: "restoring" },
        activeEvents: [{ ...historyEvent, state: "restoring" }],
        activeEventId: "curt-1",
      }),
    );

    render(<CurtailmentManagementPanel enableRecover />);

    await user.click(screen.getByRole("button", { name: "Request stop restore" }));
    await user.type(screen.getByRole("textbox", { name: "Reason" }), "Recovery cannot complete");
    await user.click(screen.getByRole("button", { name: "Stop restore" }));

    await waitFor(() => expect(mocks.adminTerminateCurtailment).toHaveBeenCalled());
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.getByRole("button", { name: "Request stop restore" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request abort" })).not.toBeInTheDocument();
  });

  it("treats unclassified network failures as transient and keeps stop restore available", async () => {
    const user = userEvent.setup();
    // connect-web wraps raw transport failures (fetch TypeError, connection
    // reset) as ConnectError with Code.Unknown; a network blip must not swap
    // Stop restore for the destructive Abort restore escalation.
    mocks.adminTerminateCurtailment.mockRejectedValueOnce(
      Object.assign(new Error("network error"), {
        cause: new ConnectError("network error", Code.Unknown),
      }),
    );
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, isAutomationOwned: true, state: "restoring" },
        activeEvents: [{ ...historyEvent, state: "restoring" }],
        activeEventId: "curt-1",
      }),
    );

    render(<CurtailmentManagementPanel enableRecover />);

    await user.click(screen.getByRole("button", { name: "Request stop restore" }));
    await user.type(screen.getByRole("textbox", { name: "Reason" }), "Recovery cannot complete");
    await user.click(screen.getByRole("button", { name: "Stop restore" }));

    await waitFor(() => expect(mocks.adminTerminateCurtailment).toHaveBeenCalled());
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.getByRole("button", { name: "Request stop restore" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request abort" })).not.toBeInTheDocument();
  });

  it("hides stop restore for non-automation restoring events", () => {
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, isAutomationOwned: false, state: "restoring" },
        activeEvents: [{ ...historyEvent, state: "restoring" }],
        activeEventId: "curt-1",
      }),
    );

    render(<CurtailmentManagementPanel enableRecover />);

    expect(screen.queryByRole("button", { name: "Request stop restore" })).not.toBeInTheDocument();
  });

  it("selects non-selected restoring history events for recovery", async () => {
    const user = userEvent.setup();
    const restoringHistoryEvent = {
      ...historyEvent,
      id: "curt-restoring",
      state: "restoring",
    } as CurtailmentHistoryEvent;
    mocks.selectActiveCurtailment.mockResolvedValue({
      activeEvent: { ...activeEvent, isAutomationOwned: true, state: "restoring" },
      activeEventId: "curt-restoring",
      activeEventFormValues: null,
    });
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
        activeEvents: [{ ...historyEvent, state: "active" }, restoringHistoryEvent],
        activeEventId: "curt-1",
        historyEvents: [restoringHistoryEvent],
      }),
    );

    render(<CurtailmentManagementPanel enableRecover />);

    await user.click(screen.getByRole("button", { name: "Select history event" }));

    expect(mocks.selectActiveCurtailment).toHaveBeenCalledWith("curt-restoring", { signal: expect.any(AbortSignal) });
  });

  it("does not submit stale stop confirmations for events that are no longer active", async () => {
    const user = userEvent.setup();
    const staleEvent = { ...historyEvent, state: "active" } as CurtailmentHistoryEvent;
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
        activeEvents: [staleEvent],
        activeEventId: "curt-1",
      }),
    );

    const { rerender } = render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Request restore now" }));
    expect(screen.getByRole("dialog", { name: "restore confirmation" })).toBeInTheDocument();

    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: null,
        activeEvents: [],
        activeEventId: null,
        historyEvents: [{ ...historyEvent, state: "completed" }],
      }),
    );
    rerender(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Confirm confirmation" }));

    expect(mocks.stopCurtailment).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog", { name: "restore confirmation" })).not.toBeInTheDocument();
  });

  it("dismisses terminal active curtailments from the active status card", async () => {
    const user = userEvent.setup();
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, state: "completed" },
        activeEventId: "curt-1",
      }),
    );

    render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Dismiss restored" }));

    expect(mocks.dismissTerminalCurtailment).toHaveBeenCalledOnce();
  });

  it("skips overlapping active curtailment polls while a background refresh is still in flight", async () => {
    vi.useFakeTimers();
    const pollingSignals: AbortSignal[] = [];
    let resolvePollingRefresh: (value: typeof emptySnapshot) => void = () => undefined;
    mocks.refreshCurtailment.mockImplementation((options = {}) => {
      if (options.background && options.signal) {
        pollingSignals.push(options.signal);
        return new Promise((resolve) => {
          resolvePollingRefresh = resolve;
        });
      }
      return Promise.resolve(emptySnapshot);
    });
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, state: "restoring" },
        activeEvents: [{ ...historyEvent, state: "restoring" }],
        activeEventId: "curt-1",
      }),
    );

    try {
      render(<CurtailmentManagementPanel />);

      expect(mocks.refreshCurtailment).toHaveBeenCalledWith({ signal: expect.any(AbortSignal) });

      await vi.advanceTimersByTimeAsync(3_000);

      expect(mocks.refreshCurtailment).toHaveBeenCalledWith({
        background: true,
        signal: expect.any(AbortSignal),
      });
      expect(pollingSignals).toHaveLength(1);
      expect(pollingSignals[0].aborted).toBe(false);

      await vi.advanceTimersByTimeAsync(3_000);

      expect(pollingSignals).toHaveLength(1);
      expect(pollingSignals[0].aborted).toBe(false);

      resolvePollingRefresh(emptySnapshot);
      await vi.advanceTimersByTimeAsync(0);
      await vi.advanceTimersByTimeAsync(3_000);

      expect(pollingSignals).toHaveLength(2);
      expect(pollingSignals[0].aborted).toBe(false);
      expect(pollingSignals[1].aborted).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps polling while visible history has a non-terminal event after active clears", async () => {
    vi.useFakeTimers();
    const restoringHistoryEvent = {
      ...historyEvent,
      state: "restoring",
    } as CurtailmentHistoryEvent;
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: null,
        activeEventId: null,
        historyEvents: [restoringHistoryEvent],
      }),
    );

    try {
      render(<CurtailmentManagementPanel />);

      await vi.advanceTimersByTimeAsync(3_000);

      expect(mocks.refreshCurtailment).toHaveBeenCalledWith({
        background: true,
        signal: expect.any(AbortSignal),
      });
    } finally {
      vi.useRealTimers();
    }
  });

  it("lets user-driven history navigation supersede active polling", async () => {
    vi.useFakeTimers();
    const pollingSignals: AbortSignal[] = [];
    mocks.refreshCurtailment.mockImplementation((options = {}) => {
      if (options.background && options.signal) {
        pollingSignals.push(options.signal);
        return new Promise(() => {});
      }
      return Promise.resolve(emptySnapshot);
    });
    mocks.goToHistoryPage.mockReturnValue(new Promise(() => {}));
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, state: "restoring" },
        activeEvents: [{ ...historyEvent, state: "restoring" }],
        activeEventId: "curt-1",
      }),
    );

    try {
      render(<CurtailmentManagementPanel />);

      await vi.advanceTimersByTimeAsync(3_000);
      expect(pollingSignals).toHaveLength(1);
      expect(pollingSignals[0].aborted).toBe(false);

      fireEvent.click(screen.getByRole("button", { name: "Load page 2" }));

      expect(mocks.goToHistoryPage).toHaveBeenCalledWith(2, { signal: expect.any(AbortSignal) });
      expect(pollingSignals[0].aborted).toBe(true);

      await vi.advanceTimersByTimeAsync(3_000);

      expect(pollingSignals).toHaveLength(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("opens active curtailment management and submits updates", async () => {
    const user = userEvent.setup();
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
        activeEventId: "curt-1",
        activeEventFormValues,
      }),
    );

    render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Request edit" }));

    expect(screen.getByRole("dialog", { name: "Manage curtailment" })).toBeInTheDocument();
    expect(screen.getByTestId("modal-initial-reason")).toHaveTextContent("Grid peak");
    expect(screen.getByTestId("modal-preview")).toHaveTextContent("2 miners, 5 kW target, 6.2 kW estimated");

    await user.click(screen.getByRole("button", { name: "Submit edit" }));

    await waitFor(() =>
      expect(mocks.updateCurtailment).toHaveBeenCalledWith("curt-1", mocks.submitValues, activeEventFormValues),
    );
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Manage curtailment" })).not.toBeInTheDocument());
  });

  it("loads secondary active row detail before opening management", async () => {
    const user = userEvent.setup();
    const secondaryFormValues = {
      ...activeEventFormValues,
      reason: "Secondary grid peak",
      targetKw: "8",
    } satisfies CurtailmentSubmitValues;
    const secondaryActiveEvent = {
      ...activeEvent,
      reason: "Secondary grid peak",
      selectedMiners: 4,
      targetKw: 8,
      estimatedReductionKw: 9.1,
    } as ActiveCurtailmentEvent;
    const secondaryHistoryEvent = { ...historyEvent, id: "curt-2" } as CurtailmentHistoryEvent;
    mocks.selectActiveCurtailment.mockResolvedValueOnce({
      activeEvent: secondaryActiveEvent,
      activeEventId: "curt-2",
      activeEventFormValues: secondaryFormValues,
    });
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
        activeEventId: "curt-1",
        activeEventFormValues,
        activeEvents: [historyEvent, secondaryHistoryEvent],
        historyEvents: [secondaryHistoryEvent],
      }),
    );

    render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Manage history event" }));

    await waitFor(() =>
      expect(mocks.selectActiveCurtailment).toHaveBeenCalledWith("curt-2", { signal: expect.any(AbortSignal) }),
    );
    expect(screen.getByRole("dialog", { name: "Manage curtailment" })).toBeInTheDocument();
    expect(screen.getByTestId("modal-initial-reason")).toHaveTextContent("Secondary grid peak");
    expect(screen.getByTestId("modal-preview")).toHaveTextContent("4 miners, 8 kW target, 9.1 kW estimated");
  });

  it("does not open management when hydrated row detail is no longer updateable", async () => {
    const user = userEvent.setup();
    const restoringActiveEvent = {
      ...activeEvent,
      state: "restoring",
      reason: "Restoring grid peak",
    } as ActiveCurtailmentEvent;
    const secondaryHistoryEvent = { ...historyEvent, id: "curt-2" } as CurtailmentHistoryEvent;
    mocks.selectActiveCurtailment.mockResolvedValueOnce({
      activeEvent: restoringActiveEvent,
      activeEventId: "curt-2",
      activeEventFormValues: {
        ...activeEventFormValues,
        reason: "Restoring grid peak",
      },
    });
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
        activeEventId: "curt-1",
        activeEventFormValues,
        activeEvents: [historyEvent, secondaryHistoryEvent],
        historyEvents: [secondaryHistoryEvent],
      }),
    );

    render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Manage history event" }));

    await waitFor(() =>
      expect(mocks.selectActiveCurtailment).toHaveBeenCalledWith("curt-2", { signal: expect.any(AbortSignal) }),
    );
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Manage curtailment" })).not.toBeInTheDocument());
  });

  it("ignores stale secondary active row detail responses", async () => {
    const user = userEvent.setup();
    const firstFormValues = {
      ...activeEventFormValues,
      reason: "First selected event",
      targetKw: "6",
    } satisfies CurtailmentSubmitValues;
    const secondFormValues = {
      ...activeEventFormValues,
      reason: "Second selected event",
      targetKw: "8",
    } satisfies CurtailmentSubmitValues;
    const firstActiveEvent = {
      ...activeEvent,
      reason: "First selected event",
      targetKw: 6,
      estimatedReductionKw: 6.5,
    } as ActiveCurtailmentEvent;
    const secondActiveEvent = {
      ...activeEvent,
      reason: "Second selected event",
      selectedMiners: 4,
      targetKw: 8,
      estimatedReductionKw: 9.1,
    } as ActiveCurtailmentEvent;
    let resolveFirstSelection: (
      value: Awaited<ReturnType<UseCurtailmentApiResult["selectActiveCurtailment"]>>,
    ) => void = () => undefined;
    let resolveSecondSelection: (
      value: Awaited<ReturnType<UseCurtailmentApiResult["selectActiveCurtailment"]>>,
    ) => void = () => undefined;
    const firstHistoryEvent = { ...historyEvent, id: "curt-2" } as CurtailmentHistoryEvent;
    const secondHistoryEvent = { ...historyEvent, id: "curt-3" } as CurtailmentHistoryEvent;
    mocks.selectActiveCurtailment
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveFirstSelection = resolve;
          }),
      )
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveSecondSelection = resolve;
          }),
      );
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
        activeEventId: "curt-1",
        activeEventFormValues,
        activeEvents: [historyEvent, firstHistoryEvent, secondHistoryEvent],
        historyEvents: [firstHistoryEvent, secondHistoryEvent],
      }),
    );

    render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Manage history event" }));
    const firstSignal = mocks.selectActiveCurtailment.mock.calls[0][1].signal as AbortSignal;
    await user.click(screen.getByRole("button", { name: "Manage second history event" }));

    expect(firstSignal.aborted).toBe(true);

    resolveSecondSelection({
      activeEvent: secondActiveEvent,
      activeEventId: "curt-3",
      activeEventFormValues: secondFormValues,
    });
    await waitFor(() => expect(screen.getByRole("dialog", { name: "Manage curtailment" })).toBeInTheDocument());

    resolveFirstSelection({
      activeEvent: firstActiveEvent,
      activeEventId: "curt-2",
      activeEventFormValues: firstFormValues,
    });

    await waitFor(() => expect(screen.getByTestId("modal-initial-reason")).toHaveTextContent("Second selected event"));
    expect(screen.getByTestId("modal-preview")).toHaveTextContent("4 miners, 8 kW target, 9.1 kW estimated");
  });

  it("keeps selected-row management open when a stale secondary selection resolves", async () => {
    const user = userEvent.setup();
    const secondaryFormValues = {
      ...activeEventFormValues,
      reason: "Secondary grid peak",
      targetKw: "8",
    } satisfies CurtailmentSubmitValues;
    const secondaryActiveEvent = {
      ...activeEvent,
      reason: "Secondary grid peak",
      selectedMiners: 4,
      targetKw: 8,
      estimatedReductionKw: 9.1,
    } as ActiveCurtailmentEvent;
    let resolveSelection: (
      value: Awaited<ReturnType<UseCurtailmentApiResult["selectActiveCurtailment"]>>,
    ) => void = () => undefined;
    const secondaryHistoryEvent = { ...historyEvent, id: "curt-2" } as CurtailmentHistoryEvent;
    mocks.selectActiveCurtailment.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveSelection = resolve;
        }),
    );
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
        activeEventId: "curt-1",
        activeEventFormValues,
        activeEvents: [secondaryHistoryEvent, { ...historyEvent, id: "curt-1" }],
        historyEvents: [secondaryHistoryEvent, { ...historyEvent, id: "curt-1" }],
      }),
    );

    render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Manage history event" }));
    const selectionSignal = mocks.selectActiveCurtailment.mock.calls[0][1].signal as AbortSignal;
    await user.click(screen.getByRole("button", { name: "Manage second history event" }));

    expect(selectionSignal.aborted).toBe(true);
    expect(screen.getByRole("dialog", { name: "Manage curtailment" })).toBeInTheDocument();
    expect(screen.getByTestId("modal-initial-reason")).toHaveTextContent("Grid peak");

    resolveSelection({
      activeEvent: secondaryActiveEvent,
      activeEventId: "curt-2",
      activeEventFormValues: secondaryFormValues,
    });

    await waitFor(() => expect(screen.getByTestId("modal-initial-reason")).toHaveTextContent("Grid peak"));
    expect(screen.getByTestId("modal-preview")).toHaveTextContent("2 miners, 5 kW target, 6.2 kW estimated");
  });

  it("keeps stop confirmation open when a stale manage selection resolves", async () => {
    const user = userEvent.setup();
    const secondaryFormValues = {
      ...activeEventFormValues,
      reason: "Secondary grid peak",
      targetKw: "8",
    } satisfies CurtailmentSubmitValues;
    const secondaryActiveEvent = {
      ...activeEvent,
      reason: "Secondary grid peak",
      selectedMiners: 4,
      targetKw: 8,
      estimatedReductionKw: 9.1,
    } as ActiveCurtailmentEvent;
    let resolveSelection: (
      value: Awaited<ReturnType<UseCurtailmentApiResult["selectActiveCurtailment"]>>,
    ) => void = () => undefined;
    const secondaryHistoryEvent = { ...historyEvent, id: "curt-2" } as CurtailmentHistoryEvent;
    mocks.selectActiveCurtailment.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveSelection = resolve;
        }),
    );
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
        activeEventId: "curt-1",
        activeEventFormValues,
        activeEvents: [secondaryHistoryEvent],
        historyEvents: [secondaryHistoryEvent],
      }),
    );

    render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Manage history event" }));
    const selectionSignal = mocks.selectActiveCurtailment.mock.calls[0][1].signal as AbortSignal;
    await user.click(screen.getByRole("button", { name: "Request restore now" }));

    expect(selectionSignal.aborted).toBe(true);
    expect(screen.getByRole("dialog", { name: "restore confirmation" })).toBeInTheDocument();

    resolveSelection({
      activeEvent: secondaryActiveEvent,
      activeEventId: "curt-2",
      activeEventFormValues: secondaryFormValues,
    });

    await waitFor(() => expect(screen.getByRole("dialog", { name: "restore confirmation" })).toBeInTheDocument());
    expect(screen.queryByRole("dialog", { name: "Manage curtailment" })).not.toBeInTheDocument();
  });

  it("keeps create flow open when a stale manage selection resolves", async () => {
    const user = userEvent.setup();
    const secondaryFormValues = {
      ...activeEventFormValues,
      reason: "Secondary grid peak",
      targetKw: "8",
    } satisfies CurtailmentSubmitValues;
    const secondaryActiveEvent = {
      ...activeEvent,
      reason: "Secondary grid peak",
      selectedMiners: 4,
      targetKw: 8,
      estimatedReductionKw: 9.1,
    } as ActiveCurtailmentEvent;
    let resolveSelection: (
      value: Awaited<ReturnType<UseCurtailmentApiResult["selectActiveCurtailment"]>>,
    ) => void = () => undefined;
    const secondaryHistoryEvent = { ...historyEvent, id: "curt-2" } as CurtailmentHistoryEvent;
    mocks.selectActiveCurtailment.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveSelection = resolve;
        }),
    );
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
        activeEventId: "curt-1",
        activeEventFormValues,
        activeEvents: [historyEvent, secondaryHistoryEvent],
        historyEvents: [secondaryHistoryEvent],
      }),
    );

    render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Manage history event" }));
    const selectionSignal = mocks.selectActiveCurtailment.mock.calls[0][1].signal as AbortSignal;
    await user.click(screen.getByRole("button", { name: "Run curtailment" }));

    expect(selectionSignal.aborted).toBe(true);
    expect(screen.getByRole("dialog", { name: "New curtailment" })).toBeInTheDocument();

    resolveSelection({
      activeEvent: secondaryActiveEvent,
      activeEventId: "curt-2",
      activeEventFormValues: secondaryFormValues,
    });

    await waitFor(() => expect(screen.getByRole("dialog", { name: "New curtailment" })).toBeInTheDocument());
    expect(screen.queryByRole("dialog", { name: "Manage curtailment" })).not.toBeInTheDocument();
  });

  it("uses estimated reduction as the edit preview target for full-fleet curtailments", async () => {
    const user = userEvent.setup();
    const { targetKw: _targetKw, ...activeEventWithoutTarget } = activeEvent;
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: {
          ...activeEventWithoutTarget,
          estimatedReductionKw: 45,
        },
        activeEventId: "curt-1",
        activeEventFormValues: {
          ...activeEventFormValues,
          curtailmentMode: "fullFleet",
          targetKw: "",
          toleranceKw: "",
        },
      }),
    );

    render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Request edit" }));

    expect(screen.getByTestId("modal-preview")).toHaveTextContent("2 miners, 45 kW target, 45 kW estimated");
  });

  it("hides curtailment management actions for users without curtailment manage permission", () => {
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
        activeEventId: "curt-1",
        activeEventFormValues,
      }),
    );

    render(<CurtailmentManagementPanel enableManage={false} />);

    expect(screen.getByTestId("active-curtailment-status")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request edit" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request restore" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request restore now" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Stop history event" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Edit settings" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Run curtailment" })).not.toBeInTheDocument();
  });

  it("keeps the edit baseline stable after active event refreshes", async () => {
    const user = userEvent.setup();
    const refreshedFormValues = {
      ...activeEventFormValues,
      reason: "Operator draft",
    } as CurtailmentSubmitValues;
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
        activeEventId: "curt-1",
        activeEventFormValues,
      }),
    );

    const { rerender } = render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Request edit" }));

    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
        activeEventId: "curt-1",
        activeEventFormValues: refreshedFormValues,
      }),
    );
    rerender(<CurtailmentManagementPanel />);

    expect(screen.getByTestId("modal-initial-reason")).toHaveTextContent("Grid peak");

    await user.click(screen.getByRole("button", { name: "Submit edit" }));

    await waitFor(() =>
      expect(mocks.updateCurtailment).toHaveBeenCalledWith("curt-1", mocks.submitValues, activeEventFormValues),
    );
  });

  it("opens stop confirmation from the management modal", async () => {
    const user = userEvent.setup();
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
        activeEventId: "curt-1",
        activeEventFormValues,
      }),
    );

    render(<CurtailmentManagementPanel />);

    await user.click(screen.getByRole("button", { name: "Request edit" }));
    await user.click(screen.getByRole("button", { name: "Stop from editor" }));

    expect(screen.queryByRole("dialog", { name: "Manage curtailment" })).not.toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "stopCurtailment confirmation" })).toBeInTheDocument();
  });

  it("loads controlled history pages and surfaces focused errors", async () => {
    const user = userEvent.setup();
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        historyCurrentPage: 1,
        historyEvents: [historyEvent],
        historyHasNextPage: true,
        historyHasPreviousPage: true,
        loadError: "Failed to load curtailment data.",
      }),
    );

    render(<CurtailmentManagementPanel />);

    expect(screen.getByText("Failed to load curtailment data.")).toBeInTheDocument();
    expect(screen.getByTestId("history-page")).toHaveTextContent("1");
    expect(screen.getByTestId("history-page-size")).toHaveTextContent("50");
    expect(screen.getByTestId("history-has-next")).toHaveTextContent("true");
    expect(screen.getByTestId("history-has-previous")).toHaveTextContent("true");

    await user.click(screen.getByRole("button", { name: "Load page 2" }));

    expect(mocks.goToHistoryPage).toHaveBeenCalledWith(2, { signal: expect.any(AbortSignal) });
  });

  it("surfaces update errors", () => {
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        updateError: "Failed to update curtailment.",
      }),
    );

    render(<CurtailmentManagementPanel />);

    expect(screen.getByText("Failed to update curtailment.")).toBeInTheDocument();
  });

  it("shows operator-friendly automation restore guard errors", () => {
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, isAutomationOwned: true },
        stopError:
          'cannot restore automation-owned curtailment event curt-1 while automation rule "test" still has OFF asserted; use force=true to override',
      }),
    );

    render(<CurtailmentManagementPanel enableManage enableRecover />);

    expect(
      screen.getByText(
        "Automation is still requesting curtailment. Use Abort to cancel this event and disable the automation before restoring miners.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/cannot restore automation-owned curtailment event/)).not.toBeInTheDocument();
  });

  it("passes status filters through to the curtailment API hook", async () => {
    const user = userEvent.setup();
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        historyEvents: [historyEvent],
        historyStatusFilters: ["active", "restoring"],
      }),
    );

    render(<CurtailmentManagementPanel />);

    expect(screen.getByTestId("history-status-filter")).toHaveTextContent("active,restoring");

    await user.click(screen.getByRole("button", { name: "Filter completed and failed" }));

    expect(mocks.setHistoryStatusFilters).toHaveBeenCalledWith(["completed", "failed"], {
      signal: expect.any(AbortSignal),
    });
  });
});
