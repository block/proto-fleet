import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import type { UseCurtailmentApiResult } from "@/protoFleet/api/useCurtailmentApi";
import type { ActiveCurtailmentEvent } from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import type { CurtailmentHistoryEvent } from "@/protoFleet/features/energy/CurtailmentHistory";
import CurtailmentManagementPanel from "@/protoFleet/features/energy/CurtailmentManagementPanel";
import type {
  CurtailmentPlanPreview,
  CurtailmentSubmitValues,
} from "@/protoFleet/features/energy/CurtailmentStartModal";

const mocks = vi.hoisted(() => ({
  dismissTerminalCurtailment: vi.fn(),
  goToHistoryPage: vi.fn(),
  refreshCurtailment: vi.fn(),
  setHistoryStatusFilter: vi.fn(),
  setHistoryStatusFilters: vi.fn(),
  startCurtailment: vi.fn(),
  stopCurtailment: vi.fn(),
  submitValues: { reason: "Grid peak" },
  updateCurtailment: vi.fn(),
  useCurtailmentApi: vi.fn(),
}));

vi.mock("@/protoFleet/api/useCurtailmentApi", () => ({
  useCurtailmentApi: () => mocks.useCurtailmentApi(),
}));

vi.mock("@/protoFleet/features/energy/ActiveCurtailmentStatus", () => ({
  default: ({
    onDismissRestored,
    onRequestEdit,
    onRequestRestore,
    onRequestStop,
  }: {
    onDismissRestored?: () => void;
    onRequestEdit?: () => void;
    onRequestRestore?: () => void;
    onRequestStop?: () => void;
  }) => (
    <div data-testid="active-curtailment-status">
      <button type="button" onClick={onDismissRestored}>
        Dismiss restored
      </button>
      <button type="button" onClick={onRequestEdit}>
        Request edit
      </button>
      <button type="button" onClick={onRequestRestore}>
        Request restore
      </button>
      <button type="button" onClick={onRequestStop}>
        Request stop
      </button>
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
    onStatusFiltersChange,
    onStopActiveEvent,
  }: {
    currentPage?: number;
    events: CurtailmentHistoryEvent[];
    hasNextPage?: boolean;
    hasPreviousPage?: boolean;
    pageSize?: number;
    selectedStatusFilters?: string[];
    onPageChange?: (page: number) => void;
    onStatusFiltersChange?: (filters: string[]) => void;
    onStopActiveEvent?: (event: CurtailmentHistoryEvent) => void | Promise<unknown>;
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
      <button type="button" disabled={events.length === 0} onClick={() => onStopActiveEvent?.(events[0])}>
        Stop history event
      </button>
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
  }: {
    initialValues?: Partial<CurtailmentSubmitValues>;
    mode?: string;
    onStopCurtailment?: () => void;
    onSubmit: (values: CurtailmentSubmitValues) => void;
    preview?: CurtailmentPlanPreview;
  }) => (
    <div role="dialog" aria-label={mode === "edit" ? "Manage curtailment" : "Plan curtailment"}>
      <div data-testid="modal-initial-reason">{initialValues?.reason ?? ""}</div>
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
  restoreBatchSize: "1",
  restoreIntervalSec: "60",
  includeMaintenance: true,
} satisfies CurtailmentSubmitValues;
const historyEvent = { id: "curt-1" } as CurtailmentHistoryEvent;

const emptySnapshot = {
  activeEvent: null,
  activeEventId: null,
  activeEventFormValues: null,
  historyEvents: [],
};

function createApiResult(overrides: Partial<UseCurtailmentApiResult> = {}): UseCurtailmentApiResult {
  return {
    activeEvent: null,
    activeEventId: null,
    historyEvents: [],
    activeEventFormValues: null,
    isLoading: false,
    isStarting: false,
    isUpdating: false,
    stoppingEventId: null,
    loadError: null,
    startError: null,
    updateError: null,
    stopError: null,
    historyCurrentPage: 0,
    historyHasNextPage: false,
    historyHasPreviousPage: false,
    historyPageSize: 50,
    historyStatusFilters: [],
    refreshCurtailment: mocks.refreshCurtailment as UseCurtailmentApiResult["refreshCurtailment"],
    goToHistoryPage: mocks.goToHistoryPage as UseCurtailmentApiResult["goToHistoryPage"],
    setHistoryStatusFilter: mocks.setHistoryStatusFilter as UseCurtailmentApiResult["setHistoryStatusFilter"],
    setHistoryStatusFilters: mocks.setHistoryStatusFilters as UseCurtailmentApiResult["setHistoryStatusFilters"],
    startCurtailment: mocks.startCurtailment as UseCurtailmentApiResult["startCurtailment"],
    dismissTerminalCurtailment:
      mocks.dismissTerminalCurtailment as UseCurtailmentApiResult["dismissTerminalCurtailment"],
    updateCurtailment: mocks.updateCurtailment as UseCurtailmentApiResult["updateCurtailment"],
    stopCurtailment: mocks.stopCurtailment as UseCurtailmentApiResult["stopCurtailment"],
    ...overrides,
  };
}

describe("CurtailmentManagementPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.refreshCurtailment.mockResolvedValue(emptySnapshot);
    mocks.goToHistoryPage.mockResolvedValue(emptySnapshot);
    mocks.setHistoryStatusFilter.mockResolvedValue(emptySnapshot);
    mocks.setHistoryStatusFilters.mockResolvedValue(emptySnapshot);
    mocks.startCurtailment.mockResolvedValue({});
    mocks.stopCurtailment.mockResolvedValue({});
    mocks.updateCurtailment.mockResolvedValue({});
    mocks.useCurtailmentApi.mockReturnValue(createApiResult());
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

    await user.click(screen.getByRole("button", { name: "Plan curtailment" }));
    await user.click(screen.getByRole("button", { name: "Submit plan" }));

    await waitFor(() => expect(mocks.startCurtailment).toHaveBeenCalledWith(mocks.submitValues));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Plan curtailment" })).not.toBeInTheDocument());

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

  it("calls stop curtailment from restore, stop, and history requests", async () => {
    const user = userEvent.setup();
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent,
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

  it("polls active curtailments on the current history page so restoring can become restored", async () => {
    vi.useFakeTimers();
    const pollingSignals: AbortSignal[] = [];
    mocks.refreshCurtailment.mockImplementation((options = {}) => {
      if (options.background && options.signal) {
        pollingSignals.push(options.signal);
      }
      return new Promise(() => {});
    });
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        activeEvent: { ...activeEvent, state: "restoring" },
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

      expect(pollingSignals).toHaveLength(2);
      expect(pollingSignals[0].aborted).toBe(true);
      expect(pollingSignals[1].aborted).toBe(false);
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
