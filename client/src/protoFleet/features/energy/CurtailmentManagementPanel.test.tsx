import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import type { UseCurtailmentApiResult } from "@/protoFleet/api/useCurtailmentApi";
import type { ActiveCurtailmentEvent } from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import type { CurtailmentHistoryEvent } from "@/protoFleet/features/energy/CurtailmentHistory";
import CurtailmentManagementPanel from "@/protoFleet/features/energy/CurtailmentManagementPanel";
import type { CurtailmentSubmitValues } from "@/protoFleet/features/energy/CurtailmentStartModal";

const mocks = vi.hoisted(() => ({
  goToHistoryPage: vi.fn(),
  refreshCurtailment: vi.fn(),
  setHistoryStatusFilter: vi.fn(),
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
    onRequestEdit,
    onRequestRestore,
    onRequestStop,
  }: {
    onRequestEdit?: () => void;
    onRequestRestore?: () => void;
    onRequestStop?: () => void;
  }) => (
    <div data-testid="active-curtailment-status">
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
    selectedStatusFilter,
    onPageChange,
    onStatusFilterChange,
    onStopActiveEvent,
  }: {
    currentPage?: number;
    events: CurtailmentHistoryEvent[];
    hasNextPage?: boolean;
    hasPreviousPage?: boolean;
    pageSize?: number;
    selectedStatusFilter?: string;
    onPageChange?: (page: number) => void;
    onStatusFilterChange?: (filter?: string) => void;
    onStopActiveEvent?: (event: CurtailmentHistoryEvent) => void | Promise<unknown>;
  }) => (
    <div data-testid="curtailment-history">
      <div data-testid="history-page">{currentPage}</div>
      <div data-testid="history-page-size">{pageSize}</div>
      <div data-testid="history-has-next">{String(hasNextPage)}</div>
      <div data-testid="history-has-previous">{String(hasPreviousPage)}</div>
      <div data-testid="history-status-filter">{selectedStatusFilter ?? ""}</div>
      <div data-testid="history-events">{events.map((event) => event.id).join(",")}</div>
      <button type="button" onClick={() => onPageChange?.(2)}>
        Load page 2
      </button>
      <button type="button" onClick={() => onStatusFilterChange?.("completed")}>
        Filter completed
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
  }: {
    initialValues?: Partial<CurtailmentSubmitValues>;
    mode?: string;
    onStopCurtailment?: () => void;
    onSubmit: (values: CurtailmentSubmitValues) => void;
  }) => (
    <div role="dialog" aria-label={mode === "edit" ? "Manage curtailment" : "Plan curtailment"}>
      <div data-testid="modal-initial-reason">{initialValues?.reason ?? ""}</div>
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

const activeEvent = { reason: "Grid peak" } as ActiveCurtailmentEvent;
const activeEventFormValues = { reason: "Grid peak", targetKw: "5" } as CurtailmentSubmitValues;
const historyEvent = { id: "curt-1" } as CurtailmentHistoryEvent;

const emptySnapshot = {
  activeEvent: null,
  activeEventId: null,
  activeEventFormValues: null,
  historyEvents: [],
};

function createApiResult(overrides: Partial<UseCurtailmentApiResult> = {}): UseCurtailmentApiResult {
  const apiResult: UseCurtailmentApiResult = {
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
    refreshCurtailment: mocks.refreshCurtailment as UseCurtailmentApiResult["refreshCurtailment"],
    goToHistoryPage: mocks.goToHistoryPage as UseCurtailmentApiResult["goToHistoryPage"],
    setHistoryStatusFilter: mocks.setHistoryStatusFilter as UseCurtailmentApiResult["setHistoryStatusFilter"],
    startCurtailment: mocks.startCurtailment as UseCurtailmentApiResult["startCurtailment"],
    updateCurtailment: mocks.updateCurtailment as UseCurtailmentApiResult["updateCurtailment"],
    stopCurtailment: mocks.stopCurtailment as UseCurtailmentApiResult["stopCurtailment"],
  };

  return Object.assign(apiResult, overrides);
}

describe("CurtailmentManagementPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.refreshCurtailment.mockResolvedValue(emptySnapshot);
    mocks.goToHistoryPage.mockResolvedValue(emptySnapshot);
    mocks.setHistoryStatusFilter.mockResolvedValue(emptySnapshot);
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

    await user.click(screen.getByRole("button", { name: "Submit edit" }));

    await waitFor(() => expect(mocks.updateCurtailment).toHaveBeenCalledWith("curt-1", mocks.submitValues));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Manage curtailment" })).not.toBeInTheDocument());
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

  it("passes status filters through to the curtailment API hook", async () => {
    const user = userEvent.setup();
    mocks.useCurtailmentApi.mockReturnValue(
      createApiResult({
        historyEvents: [historyEvent],
        historyStatusFilter: "active",
      }),
    );

    render(<CurtailmentManagementPanel />);

    expect(screen.getByTestId("history-status-filter")).toHaveTextContent("active");

    await user.click(screen.getByRole("button", { name: "Filter completed" }));

    expect(mocks.setHistoryStatusFilter).toHaveBeenCalledWith("completed", { signal: expect.any(AbortSignal) });
  });
});
