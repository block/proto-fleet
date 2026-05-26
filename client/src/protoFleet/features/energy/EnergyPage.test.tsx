import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { curtailmentChangedEvent } from "@/protoFleet/api/curtailmentNotifications";
import { CurtailmentMode, CurtailmentPriority } from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import EnergyPage from "@/protoFleet/features/energy/EnergyPage";
import { mockActiveEvent, mockHistoryEvents } from "@/protoFleet/features/energy/fixtures";
import type {
  CurtailmentActiveEvent,
  CurtailmentApi,
  CurtailmentHistoryEvent,
} from "@/protoFleet/features/energy/types";

function createHistoryEvents(count: number): CurtailmentHistoryEvent[] {
  const baseHistoryEvent = mockHistoryEvents[1];
  if (baseHistoryEvent === undefined) {
    throw new Error("Expected a base mock curtailment history event.");
  }

  return Array.from({ length: count }, (_, index) => ({
    ...baseHistoryEvent,
    id: `curt-page-${index + 1}`,
    reason: `History event ${index + 1}`,
    startedAt: `2026-04-${String(30 - (index % 20)).padStart(2, "0")}T16:02:00-04:00`,
    endedAt: `2026-04-${String(30 - (index % 20)).padStart(2, "0")}T17:05:00-04:00`,
  }));
}

function createMockApi({
  activeEvent,
  events = mockHistoryEvents,
  refreshCurtailment = vi.fn().mockResolvedValue({ activeEvent, events }),
}: {
  activeEvent?: CurtailmentActiveEvent;
  events?: CurtailmentHistoryEvent[];
  refreshCurtailment?: CurtailmentApi["refreshCurtailment"];
}): CurtailmentApi {
  return {
    activeEvent,
    events,
    isLoading: false,
    refreshCurtailment,
    startCurtailment: vi.fn().mockResolvedValue({ event: activeEvent ?? mockActiveEvent }),
    stopCurtailment: vi.fn(),
    updateCurtailmentEvent: vi.fn().mockResolvedValue({ event: activeEvent ?? mockActiveEvent }),
  };
}

describe("EnergyPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("hides active curtailment when there is no active event", () => {
    render(<EnergyPage api={createMockApi({ activeEvent: undefined, events: [] })} />);

    expect(screen.queryByText("Active curtailment")).not.toBeInTheDocument();
    expect(screen.getByText("Curtailment history")).toBeVisible();
    expect(screen.queryByRole("button", { name: "View all" })).not.toBeInTheDocument();
  });

  it("keeps planning available without showing implementation details", () => {
    render(<EnergyPage api={createMockApi({ activeEvent: undefined, events: [] })} />);

    expect(screen.queryByText((content) => content.toLowerCase().includes("implemented"))).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Plan curtailment" })).toBeEnabled();

    fireEvent.click(screen.getByRole("button", { name: "Plan curtailment" }));

    expect(screen.getByText("Plan a curtailment")).toBeInTheDocument();
  });

  it("opens the active curtailment manager from the manage button", () => {
    const api = createMockApi({ activeEvent: mockActiveEvent });

    render(<EnergyPage api={api} />);

    fireEvent.click(screen.getByRole("button", { name: "Manage" }));

    expect(screen.getByText("Manage curtailment")).toBeInTheDocument();
    expect(screen.getByLabelText("Reason")).toHaveValue(mockActiveEvent.reason);
    expect(screen.getByLabelText("Batch size (miners)")).toHaveValue(10);
    expect(screen.getByLabelText("Batch interval (sec)")).toHaveValue(120);
  });

  it("shows the active curtailment scope target in the header", () => {
    render(<EnergyPage api={createMockApi({ activeEvent: mockActiveEvent })} />);

    expect(screen.getByText("ERCOT ERS obligation (Applies to Rockdale, TX)")).toBeVisible();
  });

  it("opens the start modal when planning without an active curtailment", () => {
    render(<EnergyPage api={createMockApi({ activeEvent: undefined, events: [] })} />);

    fireEvent.click(screen.getByRole("button", { name: "Plan curtailment" }));

    expect(screen.getByText("Plan a curtailment")).toBeInTheDocument();
    expect(screen.queryByText("Curtailment already in progress")).not.toBeInTheDocument();
  });

  it("starts curtailment through the generated API request and notifies header listeners", async () => {
    const api = createMockApi({ activeEvent: undefined, events: [] });
    const dispatchEventSpy = vi.spyOn(window, "dispatchEvent");

    render(<EnergyPage api={api} />);

    fireEvent.click(screen.getByRole("button", { name: "Plan curtailment" }));
    fireEvent.change(screen.getByLabelText("Target reduction"), { target: { value: "75" } });
    fireEvent.change(screen.getByLabelText("Reason"), { target: { value: "Grid response" } });
    fireEvent.click(screen.getByText("Include miners in maintenance"));
    fireEvent.click(screen.getAllByRole("button", { name: "Start curtailment" })[0]!);

    await waitFor(() => expect(api.startCurtailment).toHaveBeenCalledOnce());

    const request = vi.mocked(api.startCurtailment).mock.calls[0]?.[0];
    expect(request).toEqual(
      expect.objectContaining({
        mode: CurtailmentMode.FIXED_KW,
        priority: CurtailmentPriority.NORMAL,
        reason: "Grid response",
        includeMaintenance: false,
        forceIncludeMaintenance: false,
      }),
    );
    expect(request?.scope.case).toBe("wholeOrg");
    expect(request?.modeParams.case).toBe("fixedKw");
    if (request?.modeParams.case !== "fixedKw") {
      throw new Error("Expected fixedKw request params");
    }
    expect(request.modeParams.value.targetKw).toBe(75);
    await waitFor(() =>
      expect(dispatchEventSpy).toHaveBeenCalledWith(expect.objectContaining({ type: curtailmentChangedEvent })),
    );
    dispatchEventSpy.mockRestore();
  });

  it("keeps successful starts successful when the follow-up refresh fails", async () => {
    const refreshCurtailment = vi
      .fn()
      .mockResolvedValueOnce({ activeEvent: undefined, events: [] })
      .mockRejectedValueOnce(new Error("refresh failed"));
    const api = createMockApi({ activeEvent: undefined, events: [], refreshCurtailment });
    const dispatchEventSpy = vi.spyOn(window, "dispatchEvent");

    render(<EnergyPage api={api} />);

    fireEvent.click(screen.getByRole("button", { name: "Plan curtailment" }));
    fireEvent.change(screen.getByLabelText("Target reduction"), { target: { value: "75" } });
    fireEvent.change(screen.getByLabelText("Reason"), { target: { value: "Grid response" } });
    fireEvent.click(screen.getByText("Include miners in maintenance"));
    fireEvent.click(screen.getAllByRole("button", { name: "Start curtailment" })[0]!);

    await waitFor(() => expect(api.startCurtailment).toHaveBeenCalledOnce());
    await waitFor(() =>
      expect(dispatchEventSpy).toHaveBeenCalledWith(expect.objectContaining({ type: curtailmentChangedEvent })),
    );
    expect(screen.queryByText("Plan a curtailment")).not.toBeInTheDocument();
    expect(await screen.findByText("refresh failed")).toBeVisible();

    dispatchEventSpy.mockRestore();
  });

  it("blocks planning while a curtailment is active", () => {
    render(<EnergyPage api={createMockApi({ activeEvent: mockActiveEvent })} />);

    fireEvent.click(screen.getByRole("button", { name: "Plan curtailment" }));

    expect(screen.getByText("Curtailment already in progress")).toBeInTheDocument();
    expect(
      screen.getByText("You can't plan a curtailment while another curtailment is active or restoring."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Plan a curtailment")).not.toBeInTheDocument();
  });

  it("blocks planning while a curtailment is restoring", () => {
    render(
      <EnergyPage
        api={createMockApi({
          activeEvent: { ...mockActiveEvent, state: "restoring" },
        })}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Plan curtailment" }));

    expect(screen.getByText("Curtailment already in progress")).toBeInTheDocument();
    expect(screen.queryByText("Plan a curtailment")).not.toBeInTheDocument();
  });

  it("opens an edit curtailment modal from details and saves restore changes", async () => {
    const api = createMockApi({ activeEvent: mockActiveEvent });

    render(<EnergyPage api={api} />);

    const activeHistoryRow = screen
      .getAllByText(mockActiveEvent.reason)
      .find((element) => element.closest("tr"))
      ?.closest("tr");
    fireEvent.click(activeHistoryRow!);
    fireEvent.click(within(screen.getByTestId("modal")).getByRole("button", { name: "Manage" }));

    expect(screen.getByText("Manage curtailment")).toBeInTheDocument();
    expect(screen.getByLabelText("Reason")).toHaveValue(mockActiveEvent.reason);
    expect(screen.queryByLabelText("Target reduction")).not.toBeInTheDocument();
    expect(screen.queryByText("Apply to")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Batch size (miners)")).toHaveValue(10);
    expect(screen.getByLabelText("Batch interval (sec)")).toHaveValue(120);

    fireEvent.change(screen.getByLabelText("Batch size (miners)"), { target: { value: "12" } });
    fireEvent.change(screen.getByLabelText("Batch interval (sec)"), { target: { value: "90" } });
    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0]!);

    await waitFor(() => expect(api.updateCurtailmentEvent).toHaveBeenCalled());
    expect(api.updateCurtailmentEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        eventUuid: mockActiveEvent.id,
        reason: mockActiveEvent.reason,
        restoreBatchSize: 12,
        restoreBatchIntervalSec: 90,
      }),
    );
  });

  it("opens an edit curtailment modal from the active card", () => {
    render(<EnergyPage api={createMockApi({ activeEvent: mockActiveEvent })} />);

    fireEvent.click(screen.getByRole("button", { name: "Manage" }));

    expect(screen.getByText("Manage curtailment")).toBeInTheDocument();
    expect(screen.getByLabelText("Reason")).toHaveValue(mockActiveEvent.reason);
  });

  it("does not offer a restore-stop action when the API is already restoring", () => {
    render(
      <EnergyPage
        api={createMockApi({
          activeEvent: {
            ...mockActiveEvent,
            state: "restoring",
            rollups: [
              { state: "resolved", count: 8 },
              { state: "confirmed", count: 10 },
            ],
          },
          events: [],
        })}
      />,
    );

    expect(screen.getByText("Restoring")).toBeVisible();
    expect(screen.getByRole("button", { name: "Manage" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Stop" })).not.toBeInTheDocument();
    expect(screen.queryByText("Stop restoration?")).not.toBeInTheDocument();
    expect(screen.queryByText("Stop curtailment?")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Start restore" })).not.toBeInTheDocument();
  });

  it("opens curtailment details from history row clicks", () => {
    render(<EnergyPage api={createMockApi({ activeEvent: undefined, events: mockHistoryEvents })} />);

    const historyRow = screen.getByText("Grid peak call").closest("tr");
    expect(historyRow).toHaveClass("hover:bg-core-primary-5");

    fireEvent.click(historyRow!);

    expect(screen.getByText("Curtailment detail")).toBeInTheDocument();
    expect(within(screen.getByTestId("modal")).getByText("Grid peak call")).toBeInTheDocument();
    expect(within(screen.getByTestId("modal")).getByText("curt-1039")).toBeInTheDocument();
    expect(within(screen.getByTestId("modal")).queryByRole("button", { name: "Edit" })).not.toBeInTheDocument();
    expect(
      within(screen.getByTestId("modal")).queryByRole("button", { name: "Stop curtailment" }),
    ).not.toBeInTheDocument();
  });

  it("shows active miner status as curtailed percentage", () => {
    render(<EnergyPage api={createMockApi({ activeEvent: mockActiveEvent })} />);

    expect(screen.getByText("89% curtailed")).toBeVisible();
    expect(screen.queryByText(/compliant/i)).not.toBeInTheDocument();
  });

  it("filters history by selected status", async () => {
    const api = createMockApi({ activeEvent: undefined, events: mockHistoryEvents });

    render(<EnergyPage api={api} />);

    fireEvent.click(screen.getByTestId("filter-dropdown-Status"));
    fireEvent.click(screen.getByTestId("filter-option-completed"));

    expect(screen.getByTestId("active-filter-status")).toHaveTextContent("Completed");
    expect(screen.getByText("Grid peak call")).toBeVisible();
    expect(screen.queryByText("Manual test")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("filter-option-cancelled"));

    expect(screen.getByTestId("active-filter-status-edit")).toHaveTextContent("2 statuses");
    expect(screen.getByText("Manual test")).toBeVisible();

    fireEvent.click(screen.getByTestId("active-filter-status-clear"));

    expect(screen.queryByTestId("active-filter-status")).not.toBeInTheDocument();
  });

  it("uses the shared no-results empty state for filtered curtailment history", async () => {
    const api = createMockApi({ activeEvent: undefined, events: [] });

    render(<EnergyPage api={api} />);

    fireEvent.click(screen.getByTestId("filter-dropdown-Status"));
    fireEvent.click(screen.getByTestId("filter-option-completed"));

    expect(screen.getByText("No results")).toBeVisible();
    expect(screen.getByText("Try adjusting or clearing your filters.")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Clear all filters" }));

    expect(screen.queryByTestId("active-filter-status")).not.toBeInTheDocument();
  });

  it("paginates curtailment history using the miner table page size and controls", () => {
    render(<EnergyPage api={createMockApi({ activeEvent: undefined, events: createHistoryEvents(52) })} />);

    expect(screen.getByText("Showing 1-50 of 52 curtailment events")).toBeVisible();
    expect(screen.getByText("History event 1")).toBeVisible();
    expect(screen.getByText("History event 50")).toBeVisible();
    expect(screen.getByRole("button", { name: "Previous page" })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "Next page" }));

    expect(screen.getByText("Showing 51-52 of 52 curtailment events")).toBeVisible();
    expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
  });
});
