import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import type { UseCurtailmentApiResult } from "@/protoFleet/api/useCurtailmentApi";
import type { ActiveCurtailmentEvent } from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import type { CurtailmentHistoryEvent } from "@/protoFleet/features/energy/CurtailmentHistory";
import CurtailmentManagementPanel from "@/protoFleet/features/energy/CurtailmentManagementPanel";
import type {
  CurtailmentPlanPreview,
  CurtailmentResponseProfileOption,
  CurtailmentSubmitValues,
} from "@/protoFleet/features/energy/CurtailmentStartModal";

const mocks = vi.hoisted(() => ({
  adminTerminateCurtailment: vi.fn(),
  dismissTerminalCurtailment: vi.fn(),
  goToHistoryPage: vi.fn(),
  navigate: vi.fn(),
  refreshCurtailment: vi.fn(),
  selectActiveCurtailment: vi.fn(),
  setHistoryStatusFilter: vi.fn(),
  setHistoryStatusFilters: vi.fn(),
  startCurtailment: vi.fn(),
  stopCurtailment: vi.fn(),
  submitValues: { reason: "Grid peak" },
  updateCurtailment: vi.fn(),
  useCurtailmentApi: vi.fn(),
  useCurtailmentResponseProfiles: vi.fn(),
}));

vi.mock("@/protoFleet/api/useCurtailmentApi", () => ({
  adminTerminateReasonRequiredMessage: "Enter a reason before terminating the event.",
  useCurtailmentApi: () => mocks.useCurtailmentApi(),
}));

vi.mock("@/protoFleet/api/useCurtailmentResponseProfiles", () => ({
  default: () => mocks.useCurtailmentResponseProfiles(),
}));

vi.mock("react-router-dom", () => ({
  useNavigate: () => mocks.navigate,
}));

vi.mock("@/protoFleet/features/energy/ActiveCurtailmentStatus", () => ({
  default: ({
    onDismissRestored,
    onRequestAdminTerminate,
    onRequestEdit,
    onRequestForceRestore,
    onRequestRestore,
    onRequestStop,
  }: {
    onDismissRestored?: () => void;
    onRequestAdminTerminate?: () => void;
    onRequestEdit?: () => void;
    onRequestForceRestore?: () => void;
    onRequestRestore?: () => void;
    onRequestStop?: () => void;
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
      {onRequestForceRestore ? (
        <button type="button" onClick={onRequestForceRestore}>
          Request force restore
        </button>
      ) : null}
      {onRequestRestore ? (
        <button type="button" onClick={onRequestRestore}>
          Request restore
        </button>
      ) : null}
      {onRequestStop ? (
        <button type="button" onClick={onRequestStop}>
          Request stop
        </button>
      ) : null}
      {onRequestAdminTerminate ? (
        <button type="button" onClick={onRequestAdminTerminate}>
          Request admin terminate
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
  default: ({
    initialValues,
    mode,
    onStopCurtailment,
    onSubmit,
    preview,
    responseProfiles,
  }: {
    initialValues?: Partial<CurtailmentSubmitValues>;
    mode?: string;
    onStopCurtailment?: () => void;
    onSubmit: (values: CurtailmentSubmitValues) => void;
    preview?: CurtailmentPlanPreview;
    responseProfiles?: CurtailmentResponseProfileOption[];
  }) => (
    <div role="dialog" aria-label={mode === "edit" ? "Manage curtailment" : "New curtailment"}>
      <div data-testid="modal-initial-reason">{initialValues?.reason ?? ""}</div>
      <div data-testid="modal-response-profiles">{responseProfiles?.map((profile) => profile.label).join(",")}</div>
      <div data-testid="modal-response-profile-values">{JSON.stringify(responseProfiles?.[0]?.values ?? {})}</div>
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
    ...overrides,
  };
}

describe("CurtailmentManagementPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
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
    mocks.updateCurtailment.mockResolvedValue({});
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
    expect(screen.getByTestId("modal-response-profile-values")).toHaveTextContent('"scopeType":"site"');
    expect(screen.getByTestId("modal-response-profile-values")).toHaveTextContent('"scopeId":"Austin, TX"');
    expect(screen.getByTestId("modal-response-profile-values")).toHaveTextContent('"siteId":"101"');
    expect(screen.getByTestId("modal-response-profile-values")).toHaveTextContent('"deviceIdentifiers":[]');
    expect(screen.getByTestId("modal-response-profile-values")).toHaveTextContent('"targetKw":"50"');
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
    await user.click(screen.getByRole("button", { name: "Request stop" }));
    expect(screen.getByRole("dialog", { name: "stopCurtailment confirmation" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Confirm confirmation" }));
    await waitFor(() => expect(mocks.stopCurtailment).toHaveBeenCalledTimes(2));

    await user.click(screen.getByRole("button", { name: "Stop history event" }));

    expect(mocks.stopCurtailment).toHaveBeenLastCalledWith("curt-1");
  });

  it("force restores automation-owned events only for admins", async () => {
    const user = userEvent.setup();
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, isAutomationOwned: true },
        activeEvents: [{ ...historyEvent, state: "active" }],
        activeEventId: "curt-1",
      }),
    );

    render(<CurtailmentManagementPanel canAdminRecoverCurtailment />);

    await user.click(screen.getByRole("button", { name: "Request restore" }));
    expect(screen.getByRole("dialog", { name: "restore confirmation" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Confirm confirmation" }));
    await waitFor(() => expect(mocks.stopCurtailment).toHaveBeenCalledWith("curt-1"));

    await user.click(screen.getByRole("button", { name: "Request force restore" }));
    expect(screen.getByRole("dialog", { name: "forceRestore confirmation" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Confirm confirmation" }));

    await waitFor(() => expect(mocks.stopCurtailment).toHaveBeenCalledWith("curt-1", { force: true }));
  });

  it("hides force recovery controls from non-admin curtailment managers", () => {
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, isAutomationOwned: true },
        activeEventId: "curt-1",
      }),
    );

    render(<CurtailmentManagementPanel canAdminRecoverCurtailment={false} />);

    expect(screen.queryByRole("button", { name: "Request force restore" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request admin terminate" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Request restore" })).toBeInTheDocument();
  });

  it("admin terminates restoring events with target state and required reason", async () => {
    const user = userEvent.setup();
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, state: "restoring" },
        activeEvents: [{ ...historyEvent, state: "restoring" }],
        activeEventId: "curt-1",
      }),
    );

    render(<CurtailmentManagementPanel canAdminRecoverCurtailment />);

    await user.click(screen.getByRole("button", { name: "Request admin terminate" }));
    await user.click(screen.getByRole("button", { name: "Terminate event" }));

    expect(screen.getByText("Enter a reason before terminating the event.")).toBeInTheDocument();
    expect(mocks.adminTerminateCurtailment).not.toHaveBeenCalled();

    await user.click(screen.getByLabelText("Failed"));
    await user.type(screen.getByRole("textbox", { name: "Reason" }), "Recovered from stale MQTT source");
    await user.click(screen.getByRole("button", { name: "Terminate event" }));

    await waitFor(() =>
      expect(mocks.adminTerminateCurtailment).toHaveBeenCalledWith("curt-1", {
        reason: "Recovered from stale MQTT source",
        targetState: "failed",
      }),
    );
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

    await user.click(screen.getByRole("button", { name: "Request stop" }));
    expect(screen.getByRole("dialog", { name: "stopCurtailment confirmation" })).toBeInTheDocument();

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
    expect(screen.queryByRole("dialog", { name: "stopCurtailment confirmation" })).not.toBeInTheDocument();
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
    await user.click(screen.getByRole("button", { name: "Request stop" }));

    expect(selectionSignal.aborted).toBe(true);
    expect(screen.getByRole("dialog", { name: "stopCurtailment confirmation" })).toBeInTheDocument();

    resolveSelection({
      activeEvent: secondaryActiveEvent,
      activeEventId: "curt-2",
      activeEventFormValues: secondaryFormValues,
    });

    await waitFor(() =>
      expect(screen.getByRole("dialog", { name: "stopCurtailment confirmation" })).toBeInTheDocument(),
    );
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

    render(<CurtailmentManagementPanel canManageCurtailment={false} />);

    expect(screen.getByTestId("active-curtailment-status")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request edit" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request restore" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Request stop" })).not.toBeInTheDocument();
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
