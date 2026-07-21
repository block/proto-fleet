import { render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import CurtailmentHistory from "@/protoFleet/features/energy/CurtailmentHistory";
import { mockCurtailmentHistoryEvents } from "@/protoFleet/features/energy/CurtailmentHistory.fixtures";

const testDateTimeFormatter = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  hour: "numeric",
  minute: "2-digit",
});

function getRenderedRows(): HTMLElement[] {
  return screen.queryAllByTestId(/^curtailment-history-row-/);
}

function createPendingStopRequest(): Promise<void> {
  return new Promise(() => undefined);
}

function createRejectableStopRequest(): {
  stopRequest: Promise<void>;
  rejectStopRequest: (error: Error) => void;
} {
  let rejectStopRequest: (error: Error) => void = () => undefined;
  const stopRequest = new Promise<void>((_, reject) => {
    rejectStopRequest = reject;
  });

  return { stopRequest, rejectStopRequest };
}

describe("CurtailmentHistory", () => {
  it("renders history rows with pagination", async () => {
    const user = userEvent.setup();
    render(<CurtailmentHistory events={mockCurtailmentHistoryEvents} pageSize={2} />);

    expect(screen.getByText("Curtailment history")).toBeInTheDocument();
    expect(screen.getByText("ERCOT ERS obligation")).toBeInTheDocument();
    expect(screen.getByText("Grid peak call")).toBeInTheDocument();
    expect(screen.getByText("Showing 1–2 of 4 curtailment events")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Next page" }));

    expect(screen.getByText("High price zone")).toBeInTheDocument();
    expect(screen.getByText("Manual test")).toBeInTheDocument();
    expect(screen.getByText("Showing 3–4 of 4 curtailment events")).toBeInTheDocument();
  });

  it("uses controlled cursor pagination without requiring a total count", async () => {
    const user = userEvent.setup();
    const onPageChange = vi.fn();
    render(
      <CurtailmentHistory
        events={mockCurtailmentHistoryEvents.slice(0, 2)}
        pageSize={2}
        currentPage={1}
        hasPreviousPage
        hasNextPage
        onPageChange={onPageChange}
      />,
    );

    expect(screen.getByText("Showing 3–4 curtailment events")).toBeInTheDocument();
    expect(screen.getByTestId("filter-dropdown-Status")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Target vs actual" })).not.toBeInTheDocument();
    expect(screen.getByText("Target vs actual")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Next page" }));
    expect(onPageChange).toHaveBeenLastCalledWith(2);

    await user.click(screen.getByRole("button", { name: "Previous page" }));
    expect(onPageChange).toHaveBeenLastCalledWith(0);
  });

  it("falls back to the default page size when pageSize is not finite", () => {
    render(<CurtailmentHistory events={mockCurtailmentHistoryEvents} pageSize={Number.NaN} />);

    expect(screen.getByText("Showing 1–4 of 4 curtailment events")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
  });

  it("renders non-sortable column headers", () => {
    render(<CurtailmentHistory events={mockCurtailmentHistoryEvents} pageSize={4} />);

    expect(screen.queryByRole("button", { name: "Target vs actual" })).not.toBeInTheDocument();
    expect(screen.getByText("Target vs actual")).toBeInTheDocument();
  });

  it("filters history rows by status and clears the filter", async () => {
    const user = userEvent.setup();
    render(<CurtailmentHistory events={mockCurtailmentHistoryEvents} />);

    await user.click(screen.getByTestId("filter-dropdown-Status"));
    await user.click(screen.getByTestId("filter-option-completed"));

    expect(screen.getByText("Grid peak call")).toBeInTheDocument();
    expect(screen.queryByText("ERCOT ERS obligation")).not.toBeInTheDocument();
    expect(screen.getByTestId("active-filter-status")).toBeInTheDocument();

    await user.click(screen.getByLabelText("Clear Status filter"));

    expect(screen.getByText("ERCOT ERS obligation")).toBeInTheDocument();
    expect(getRenderedRows()).toHaveLength(mockCurtailmentHistoryEvents.length);
  });

  it("delegates controlled multi-status filter changes without filtering the current page locally", async () => {
    const user = userEvent.setup();
    const onStatusFiltersChange = vi.fn();
    render(
      <CurtailmentHistory
        events={mockCurtailmentHistoryEvents.slice(0, 2)}
        selectedStatusFilters={["completed"]}
        onPageChange={vi.fn()}
        onStatusFiltersChange={onStatusFiltersChange}
      />,
    );

    await user.click(screen.getByTestId("filter-dropdown-Status"));
    await user.click(screen.getByTestId("filter-option-failed"));

    expect(onStatusFiltersChange).toHaveBeenCalledWith(["completed", "failed"]);
    expect(screen.getByText("ERCOT ERS obligation")).toBeInTheDocument();
  });

  it("renders high-priority events with singular miner counts", async () => {
    const user = userEvent.setup();
    const highPriorityEvent = {
      ...mockCurtailmentHistoryEvents[0],
      id: "curt-single-miner",
      priority: "high" as const,
      selectedMiners: 1,
    };

    render(<CurtailmentHistory events={[highPriorityEvent]} />);

    expect(screen.getByText("1 miner")).toBeInTheDocument();

    await user.click(screen.getByTestId("curtailment-history-row-curt-single-miner"));

    const modal = screen.getByTestId("modal");
    expect(within(modal).getByText("Type")).toBeInTheDocument();
    expect(within(modal).getByText("High")).toBeInTheDocument();
  });

  it("includes facility fans in historical applies to summaries when present", async () => {
    const user = userEvent.setup();
    const eventWithFans = {
      ...mockCurtailmentHistoryEvents[0],
      facilityFanDeviceCount: 2,
    };

    render(<CurtailmentHistory events={[eventWithFans]} />);

    const row = screen.getByTestId("curtailment-history-row-curt-1042");
    expect(within(row).getByText("Rockdale, TX")).toBeVisible();
    expect(within(row).getByText("18 miners, 2 fans")).toBeVisible();

    await user.click(row);

    const modal = screen.getByTestId("modal");
    expect(within(modal).getByText("Rockdale, TX")).toBeInTheDocument();
    expect(within(modal).getByText("18 miners, 2 fans")).toBeInTheDocument();
  });

  it("renders unavailable target metrics without misleading zero values", async () => {
    const user = userEvent.setup();
    const summaryOnlyEvent = {
      ...mockCurtailmentHistoryEvents[0],
      id: "curt-summary-only",
      selectedMiners: 0,
      estimatedReductionKw: 0,
      targetKw: undefined,
      targetMetricsAvailable: false,
    };

    render(<CurtailmentHistory events={[summaryOnlyEvent]} />);

    const row = screen.getByTestId("curtailment-history-row-curt-summary-only");
    expect(within(row).getAllByText("Target details unavailable")).toHaveLength(2);
    expect(within(row).queryByText("0 miners")).not.toBeInTheDocument();
    expect(within(row).queryByText("0.0 kW / 0.0 kW")).not.toBeInTheDocument();

    await user.click(row);

    const modal = screen.getByTestId("modal");
    expect(within(modal).getAllByText("Target details unavailable")).toHaveLength(2);
  });

  it("renders pending events without a start time", async () => {
    const user = userEvent.setup();
    const onStopActiveEvent = vi.fn();
    const pendingEvent = {
      ...mockCurtailmentHistoryEvents[0],
      id: "curt-pending",
      reason: "Queued curtailment",
      state: "pending" as const,
      startedAt: "",
    };

    render(
      <CurtailmentHistory events={[pendingEvent]} activeEventId="curt-pending" onStopActiveEvent={onStopActiveEvent} />,
    );

    expect(screen.getByText("Waiting to start")).toBeInTheDocument();

    const pendingRow = screen.getByTestId("curtailment-history-row-curt-pending");
    await user.click(within(pendingRow).getByRole("button", { name: "Stop Queued curtailment" }));

    expect(onStopActiveEvent).not.toHaveBeenCalled();
    expect(screen.getByText("Stop curtailment?")).toBeInTheDocument();
    expect(screen.queryByTestId("modal")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Confirm stop" }));

    expect(onStopActiveEvent).toHaveBeenCalledWith(pendingEvent);

    await user.click(pendingRow);

    const modal = screen.getByTestId("modal");
    expect(within(modal).getByText("Queued curtailment")).toBeInTheDocument();
    expect(within(modal).getByText("Not started yet")).toBeInTheDocument();

    expect(within(modal).getByRole("button", { name: "Stop curtailment" })).toBeDisabled();
    expect(onStopActiveEvent).toHaveBeenCalledTimes(1);
  });

  it("uses the created time as the started detail for ended events missing startedAt", async () => {
    const user = userEvent.setup();
    const completedEvent = {
      ...mockCurtailmentHistoryEvents[0],
      id: "curt-created-started-detail",
      reason: "Completed curtailment",
      state: "completed" as const,
      startedAt: "",
      createdAt: "2026-04-30T13:56:00-04:00",
      endedAt: "2026-04-30T14:12:00-04:00",
    };
    const expectedCreatedAt = testDateTimeFormatter.format(new Date(completedEvent.createdAt));

    render(<CurtailmentHistory events={[completedEvent]} />);

    await user.click(screen.getByTestId("curtailment-history-row-curt-created-started-detail"));

    const modal = screen.getByTestId("modal");
    expect(within(modal).getByText("Started")).toBeInTheDocument();
    expect(within(modal).getByText(expectedCreatedAt)).toBeInTheDocument();
    expect(within(modal).queryByText("Not started yet")).not.toBeInTheDocument();
    expect(within(modal).queryByText("Created")).not.toBeInTheDocument();
    expect(within(modal).queryByText(`Created ${expectedCreatedAt}`)).not.toBeInTheDocument();
  });

  it("renders injected active rows with their display state", async () => {
    const user = userEvent.setup();
    const curtailingPendingEvent = {
      ...mockCurtailmentHistoryEvents[0],
      id: "curt-display-state",
      reason: "Dispatch underway",
      state: "pending" as const,
      displayState: "curtailing" as const,
      startedAt: "",
    };

    render(<CurtailmentHistory events={[curtailingPendingEvent]} activeEventId={curtailingPendingEvent.id} />);

    const activeRow = screen.getByTestId("curtailment-history-row-curt-display-state");
    expect(within(activeRow).getByText("Curtailing")).toBeInTheDocument();
    expect(within(activeRow).queryByText("Pending")).not.toBeInTheDocument();
    expect(within(activeRow).getByText("Time unavailable")).toBeInTheDocument();

    await user.click(activeRow);

    const modal = screen.getByTestId("modal");
    expect(within(modal).getByText("Curtailing")).toBeInTheDocument();
    expect(within(modal).queryByText("Pending")).not.toBeInTheDocument();
  });

  it("opens row details from an empty actions cell", async () => {
    const user = userEvent.setup();
    render(<CurtailmentHistory events={mockCurtailmentHistoryEvents} />);

    const completedRow = screen.getByTestId("curtailment-history-row-curt-1039");
    const actionsCell = completedRow.querySelector("td:last-child");

    expect(actionsCell).not.toBeNull();

    await user.click(actionsCell as HTMLElement);

    const modal = screen.getByTestId("modal");
    expect(within(modal).getByText("Grid peak call")).toBeInTheDocument();
  });

  it("keeps an open detail modal synced to event updates", async () => {
    const user = userEvent.setup();
    const stopRequest = createPendingStopRequest();
    const onStopActiveEvent = vi.fn(() => stopRequest);
    const activeEvent = mockCurtailmentHistoryEvents[0];
    const completedEvent = {
      ...activeEvent,
      state: "completed" as const,
      endedAt: "2026-04-30T14:25:00-04:00",
    };
    const { rerender } = render(
      <CurtailmentHistory
        events={[activeEvent]}
        activeEventId={activeEvent.id}
        onStopActiveEvent={onStopActiveEvent}
      />,
    );

    await user.click(screen.getByTestId(`curtailment-history-row-${activeEvent.id}`));

    const stopButton = screen.getByRole("button", { name: "Stop curtailment" });
    expect(stopButton).toBeInTheDocument();

    await user.click(stopButton);

    expect(screen.getByText("Stop curtailment?")).toBeInTheDocument();
    expect(onStopActiveEvent).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Confirm stop" }));

    expect(onStopActiveEvent).toHaveBeenCalledWith(activeEvent);
    expect(stopButton).toBeDisabled();

    rerender(
      <CurtailmentHistory
        events={[completedEvent]}
        activeEventId={activeEvent.id}
        onStopActiveEvent={() => undefined}
      />,
    );

    const modal = screen.getByTestId("modal");
    expect(within(modal).getByText("Completed")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Stop curtailment" })).not.toBeInTheDocument();
  });

  it("opens row stop actions in the stop confirmation dialog", async () => {
    const user = userEvent.setup();
    const stopRequest = createPendingStopRequest();
    const onStopActiveEvent = vi.fn(() => stopRequest);

    render(
      <CurtailmentHistory
        events={mockCurtailmentHistoryEvents}
        activeEventId="curt-1042"
        onStopActiveEvent={onStopActiveEvent}
      />,
    );

    const activeRow = screen.getByTestId("curtailment-history-row-curt-1042");
    const stopButton = within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" });

    expect(screen.queryByRole("button", { name: "View ERCOT ERS obligation" })).not.toBeInTheDocument();
    expect(stopButton).toHaveTextContent("Stop");
    expect(stopButton.querySelector("svg")).toBeNull();

    await user.click(stopButton);

    expect(onStopActiveEvent).not.toHaveBeenCalled();
    expect(screen.getByText("Stop curtailment?")).toBeInTheDocument();
    expect(screen.queryByTestId("modal")).not.toBeInTheDocument();
    expect(within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" })).not.toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Confirm stop" }));

    expect(onStopActiveEvent).toHaveBeenCalledWith(mockCurtailmentHistoryEvents[0]);
    expect(within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" })).toBeDisabled();
  });

  it("keeps stop actions disabled when a confirmed stop handler returns synchronously", async () => {
    const user = userEvent.setup();
    const onStopActiveEvent = vi.fn();

    render(
      <CurtailmentHistory
        events={mockCurtailmentHistoryEvents}
        activeEventId="curt-1042"
        onStopActiveEvent={onStopActiveEvent}
      />,
    );

    const activeRow = screen.getByTestId("curtailment-history-row-curt-1042");
    await user.click(within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" }));
    await user.click(screen.getByRole("button", { name: "Confirm stop" }));

    expect(onStopActiveEvent).toHaveBeenCalledWith(mockCurtailmentHistoryEvents[0]);
    expect(within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" })).toBeDisabled();

    await user.click(activeRow);

    const modal = screen.getByTestId("modal");
    expect(within(modal).getByRole("button", { name: "Stop curtailment" })).toBeDisabled();
  });

  it("re-enables stop actions when a confirmed stop handler throws synchronously", async () => {
    const user = userEvent.setup();
    const onStopActiveEvent = vi.fn(() => {
      throw new Error("Stop request failed");
    });

    render(
      <CurtailmentHistory
        events={mockCurtailmentHistoryEvents}
        activeEventId="curt-1042"
        onStopActiveEvent={onStopActiveEvent}
      />,
    );

    const activeRow = screen.getByTestId("curtailment-history-row-curt-1042");
    await user.click(within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" }));
    await user.click(screen.getByRole("button", { name: "Confirm stop" }));

    expect(onStopActiveEvent).toHaveBeenCalledWith(mockCurtailmentHistoryEvents[0]);
    expect(screen.queryByText("Stop curtailment?")).not.toBeInTheDocument();
    expect(within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" })).not.toBeDisabled();
  });

  it("keeps stop actions enabled while confirmation is awaiting a decision", async () => {
    const user = userEvent.setup();
    const onStopActiveEvent = vi.fn();

    render(
      <CurtailmentHistory
        events={mockCurtailmentHistoryEvents}
        activeEventId="curt-1042"
        onStopActiveEvent={onStopActiveEvent}
      />,
    );

    const activeRow = screen.getByTestId("curtailment-history-row-curt-1042");
    await user.click(within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" }));

    expect(onStopActiveEvent).not.toHaveBeenCalled();
    expect(screen.getByText("Stop curtailment?")).toBeInTheDocument();
    expect(screen.queryByTestId("modal")).not.toBeInTheDocument();
    expect(within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" })).not.toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.queryByText("Stop curtailment?")).not.toBeInTheDocument();
    expect(within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" })).not.toBeDisabled();
  });

  it("routes active row manage actions through the summary modal", async () => {
    const user = userEvent.setup();
    const onManageActiveEvent = vi.fn();
    const onStopActiveEvent = vi.fn();

    render(
      <CurtailmentHistory
        events={mockCurtailmentHistoryEvents}
        activeEventId="curt-1042"
        onManageActiveEvent={onManageActiveEvent}
        onStopActiveEvent={onStopActiveEvent}
      />,
    );

    await user.click(screen.getByTestId("curtailment-history-row-curt-1042"));

    const modal = screen.getByTestId("modal");
    expect(within(modal).getByText("ERCOT ERS obligation")).toBeInTheDocument();
    expect(within(modal).getByRole("button", { name: "Stop curtailment" })).toBeInTheDocument();

    await user.click(within(modal).getByRole("button", { name: "Manage" }));

    expect(onManageActiveEvent).toHaveBeenCalledWith(mockCurtailmentHistoryEvents[0]);
    expect(onStopActiveEvent).not.toHaveBeenCalled();
    expect(screen.queryByTestId("modal")).not.toBeInTheDocument();
  });

  it("hides manage actions while a stop request is pending", async () => {
    const user = userEvent.setup();
    const stopRequest = createPendingStopRequest();
    const onManageActiveEvent = vi.fn();
    const onStopActiveEvent = vi.fn(() => stopRequest);

    render(
      <CurtailmentHistory
        events={mockCurtailmentHistoryEvents}
        activeEventId="curt-1042"
        onManageActiveEvent={onManageActiveEvent}
        onStopActiveEvent={onStopActiveEvent}
      />,
    );

    const activeRow = screen.getByTestId("curtailment-history-row-curt-1042");
    await user.click(activeRow);

    const modal = screen.getByTestId("modal");
    expect(within(modal).getByRole("button", { name: "Manage" })).toBeInTheDocument();

    await user.click(within(modal).getByRole("button", { name: "Stop curtailment" }));
    await user.click(screen.getByRole("button", { name: "Confirm stop" }));

    expect(onStopActiveEvent).toHaveBeenCalledWith(mockCurtailmentHistoryEvents[0]);
    expect(within(modal).queryByRole("button", { name: "Manage" })).not.toBeInTheDocument();
    expect(onManageActiveEvent).not.toHaveBeenCalled();
  });

  it("does not show manage actions for inactive row summaries", async () => {
    const user = userEvent.setup();

    render(
      <CurtailmentHistory
        events={mockCurtailmentHistoryEvents}
        activeEventId="curt-1042"
        onManageActiveEvent={() => undefined}
      />,
    );

    await user.click(screen.getByTestId("curtailment-history-row-curt-1039"));

    const modal = screen.getByTestId("modal");
    expect(within(modal).getByText("Grid peak call")).toBeInTheDocument();
    expect(within(modal).queryByRole("button", { name: "Manage" })).not.toBeInTheDocument();
  });

  it("does not show manage actions for restoring active row summaries", async () => {
    const user = userEvent.setup();
    const onManageActiveEvent = vi.fn();
    const restoringEvent = {
      ...mockCurtailmentHistoryEvents[0],
      id: "curt-restoring",
      reason: "Restoring event",
      state: "restoring" as const,
    };

    render(
      <CurtailmentHistory
        events={[restoringEvent]}
        activeEventId="curt-restoring"
        onManageActiveEvent={onManageActiveEvent}
      />,
    );

    await user.click(screen.getByTestId("curtailment-history-row-curt-restoring"));

    const modal = screen.getByTestId("modal");
    expect(within(modal).getByText("Restoring event")).toBeInTheDocument();
    expect(within(modal).queryByRole("button", { name: "Manage" })).not.toBeInTheDocument();
    expect(onManageActiveEvent).not.toHaveBeenCalled();
  });

  it("selects restoring active row summaries for recovery", async () => {
    const user = userEvent.setup();
    const onSelectActiveEvent = vi.fn();
    const restoringEvent = {
      ...mockCurtailmentHistoryEvents[0],
      id: "curt-restoring",
      reason: "Restoring event",
      state: "restoring" as const,
    };

    render(
      <CurtailmentHistory
        events={[restoringEvent]}
        activeEventId="curt-restoring"
        onSelectActiveEvent={onSelectActiveEvent}
      />,
    );

    await user.click(screen.getByTestId("curtailment-history-row-curt-restoring"));

    const modal = screen.getByTestId("modal");
    await user.click(within(modal).getByRole("button", { name: "View active event" }));

    expect(onSelectActiveEvent).toHaveBeenCalledWith(restoringEvent);
    expect(screen.queryByTestId("modal")).not.toBeInTheDocument();
  });

  it("routes secondary active row actions from activeEventIds", async () => {
    const user = userEvent.setup();
    const onManageActiveEvent = vi.fn();
    const onStopActiveEvent = vi.fn();
    const secondaryActiveEvent = {
      ...mockCurtailmentHistoryEvents[0],
      id: "curt-secondary-active",
      reason: "Secondary active event",
    };

    render(
      <CurtailmentHistory
        events={[mockCurtailmentHistoryEvents[0], secondaryActiveEvent]}
        activeEventId="curt-1042"
        activeEventIds={["curt-secondary-active"]}
        onManageActiveEvent={onManageActiveEvent}
        onStopActiveEvent={onStopActiveEvent}
      />,
    );

    const secondaryRow = screen.getByTestId("curtailment-history-row-curt-secondary-active");
    await user.click(secondaryRow);
    const modal = screen.getByTestId("modal");
    await user.click(within(modal).getByRole("button", { name: "Manage" }));

    expect(onManageActiveEvent).toHaveBeenCalledWith(secondaryActiveEvent);

    await user.click(within(secondaryRow).getByRole("button", { name: "Stop Secondary active event" }));
    await user.click(screen.getByRole("button", { name: "Confirm stop" }));

    expect(onStopActiveEvent).toHaveBeenCalledWith(secondaryActiveEvent);
  });

  it("keeps multiple pending stop rows disabled independently", async () => {
    const user = userEvent.setup();
    const firstStopRequest = createPendingStopRequest();
    const secondStopRequest = createPendingStopRequest();
    const onStopActiveEvent = vi.fn((event: (typeof mockCurtailmentHistoryEvents)[number]) =>
      event.id === "curt-1042" ? firstStopRequest : secondStopRequest,
    );
    const secondaryActiveEvent = {
      ...mockCurtailmentHistoryEvents[0],
      id: "curt-secondary-active",
      reason: "Secondary active event",
    };

    render(
      <CurtailmentHistory
        events={[mockCurtailmentHistoryEvents[0], secondaryActiveEvent]}
        activeEventId="curt-1042"
        activeEventIds={["curt-secondary-active"]}
        onStopActiveEvent={onStopActiveEvent}
      />,
    );

    const firstRow = screen.getByTestId("curtailment-history-row-curt-1042");
    const secondaryRow = screen.getByTestId("curtailment-history-row-curt-secondary-active");

    await user.click(within(firstRow).getByRole("button", { name: "Stop ERCOT ERS obligation" }));
    await user.click(screen.getByRole("button", { name: "Confirm stop" }));
    expect(within(firstRow).getByRole("button", { name: "Stop ERCOT ERS obligation" })).toBeDisabled();

    await user.click(within(secondaryRow).getByRole("button", { name: "Stop Secondary active event" }));
    await user.click(screen.getByRole("button", { name: "Confirm stop" }));

    expect(onStopActiveEvent).toHaveBeenCalledTimes(2);
    expect(within(firstRow).getByRole("button", { name: "Stop ERCOT ERS obligation" })).toBeDisabled();
    expect(within(secondaryRow).getByRole("button", { name: "Stop Secondary active event" })).toBeDisabled();
  });

  it("re-enables stop actions when the stop request fails", async () => {
    const user = userEvent.setup();
    const { stopRequest, rejectStopRequest } = createRejectableStopRequest();
    const onStopActiveEvent = vi.fn(() => stopRequest);

    render(
      <CurtailmentHistory
        events={mockCurtailmentHistoryEvents}
        activeEventId="curt-1042"
        onStopActiveEvent={onStopActiveEvent}
      />,
    );

    const activeRow = screen.getByTestId("curtailment-history-row-curt-1042");
    await user.click(activeRow);

    const modal = screen.getByTestId("modal");
    const modalStopButton = within(modal).getByRole("button", { name: "Stop curtailment" });

    await user.click(modalStopButton);

    expect(screen.getByText("Stop curtailment?")).toBeInTheDocument();
    expect(onStopActiveEvent).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Confirm stop" }));

    expect(onStopActiveEvent).toHaveBeenCalledWith(mockCurtailmentHistoryEvents[0]);
    expect(modalStopButton).toBeDisabled();
    expect(within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" })).toBeDisabled();

    rejectStopRequest(new Error("Stop request failed"));

    await waitFor(() => expect(modalStopButton).not.toBeDisabled());
    expect(within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" })).not.toBeDisabled();

    await user.click(modalStopButton);

    expect(screen.getByText("Stop curtailment?")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Confirm stop" }));

    expect(onStopActiveEvent).toHaveBeenCalledTimes(2);
  });

  it("keeps row activation isolated from keyboard use on the stop action", async () => {
    const user = userEvent.setup();
    const onStopActiveEvent = vi.fn();
    const onViewEvent = vi.fn();

    render(
      <CurtailmentHistory
        events={mockCurtailmentHistoryEvents}
        activeEventId="curt-1042"
        onViewEvent={onViewEvent}
        onStopActiveEvent={onStopActiveEvent}
      />,
    );

    const activeRow = screen.getByTestId("curtailment-history-row-curt-1042");
    const stopButton = within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" });

    stopButton.focus();
    await user.keyboard("{Enter}");

    expect(onStopActiveEvent).not.toHaveBeenCalled();
    expect(onViewEvent).not.toHaveBeenCalled();
    expect(screen.getByText("Stop curtailment?")).toBeInTheDocument();
    expect(screen.queryByTestId("modal")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Confirm stop" }));

    expect(onStopActiveEvent).toHaveBeenCalledWith(mockCurtailmentHistoryEvents[0]);
  });

  it("renders an empty state when there are no events", () => {
    render(<CurtailmentHistory events={[]} />);

    expect(screen.getByText("No results")).toBeInTheDocument();
    expect(screen.queryByTestId("curtailment-history-pagination")).not.toBeInTheDocument();
  });
});
