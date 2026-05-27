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
  startCurtailment: vi.fn(),
  stopCurtailment: vi.fn(),
  submitValues: { reason: "Grid peak" },
  useCurtailmentApi: vi.fn(),
}));

vi.mock("@/protoFleet/api/useCurtailmentApi", () => ({
  useCurtailmentApi: () => mocks.useCurtailmentApi(),
}));

vi.mock("@/protoFleet/features/energy/ActiveCurtailmentStatus", () => ({
  default: ({ onRequestRestore, onRequestStop }: { onRequestRestore?: () => void; onRequestStop?: () => void }) => (
    <div data-testid="active-curtailment-status">
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
    onPageChange,
    onStopActiveEvent,
  }: {
    currentPage?: number;
    events: CurtailmentHistoryEvent[];
    hasNextPage?: boolean;
    hasPreviousPage?: boolean;
    pageSize?: number;
    onPageChange?: (page: number) => void;
    onStopActiveEvent?: (event: CurtailmentHistoryEvent) => void | Promise<unknown>;
  }) => (
    <div data-testid="curtailment-history">
      <div data-testid="history-page">{currentPage}</div>
      <div data-testid="history-page-size">{pageSize}</div>
      <div data-testid="history-has-next">{String(hasNextPage)}</div>
      <div data-testid="history-has-previous">{String(hasPreviousPage)}</div>
      <div data-testid="history-events">{events.map((event) => event.id).join(",")}</div>
      <button type="button" onClick={() => onPageChange?.(2)}>
        Load page 2
      </button>
      <button type="button" disabled={events.length === 0} onClick={() => onStopActiveEvent?.(events[0])}>
        Stop history event
      </button>
    </div>
  ),
}));

vi.mock("@/protoFleet/features/energy/CurtailmentStartModal", () => ({
  default: ({ onSubmit }: { onSubmit: (values: CurtailmentSubmitValues) => void }) => (
    <div role="dialog" aria-label="Plan curtailment">
      <button type="button" onClick={() => onSubmit(mocks.submitValues as CurtailmentSubmitValues)}>
        Submit plan
      </button>
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
const historyEvent = { id: "curt-1" } as CurtailmentHistoryEvent;

const emptySnapshot = {
  activeEvent: null,
  activeEventId: null,
  historyEvents: [],
};

function createApiResult(overrides: Partial<UseCurtailmentApiResult> = {}): UseCurtailmentApiResult {
  return {
    activeEvent: null,
    activeEventId: null,
    historyEvents: [],
    isLoading: false,
    isStarting: false,
    stoppingEventId: null,
    loadError: null,
    startError: null,
    stopError: null,
    historyCurrentPage: 0,
    historyHasNextPage: false,
    historyHasPreviousPage: false,
    historyPageSize: 50,
    refreshCurtailment: mocks.refreshCurtailment as UseCurtailmentApiResult["refreshCurtailment"],
    goToHistoryPage: mocks.goToHistoryPage as UseCurtailmentApiResult["goToHistoryPage"],
    startCurtailment: mocks.startCurtailment as UseCurtailmentApiResult["startCurtailment"],
    stopCurtailment: mocks.stopCurtailment as UseCurtailmentApiResult["stopCurtailment"],
    ...overrides,
  };
}

describe("CurtailmentManagementPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.refreshCurtailment.mockResolvedValue(emptySnapshot);
    mocks.goToHistoryPage.mockResolvedValue(emptySnapshot);
    mocks.startCurtailment.mockResolvedValue({});
    mocks.stopCurtailment.mockResolvedValue({});
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
});
