import { render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import CurtailmentHistory from "@/protoFleet/features/energy/CurtailmentHistory";
import { mockCurtailmentHistoryEvents } from "@/protoFleet/features/energy/CurtailmentHistory.fixtures";

function getRenderedRows(): HTMLElement[] {
  return screen.queryAllByTestId(/^curtailment-history-row-/);
}

describe("CurtailmentHistory", () => {
  it("renders history rows with pagination", async () => {
    const user = userEvent.setup();
    render(<CurtailmentHistory events={mockCurtailmentHistoryEvents} pageSize={2} />);

    expect(screen.getByText("Curtailment history")).toBeInTheDocument();
    expect(screen.getByText("ERCOT ERS obligation")).toBeInTheDocument();
    expect(screen.getByText("Grid peak call")).toBeInTheDocument();
    expect(screen.getByText("Showing 1-2 of 4 curtailment events")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Next page" }));

    expect(screen.getByText("High price zone")).toBeInTheDocument();
    expect(screen.getByText("Manual test")).toBeInTheDocument();
    expect(screen.getByText("Showing 3-4 of 4 curtailment events")).toBeInTheDocument();
  });

  it("falls back to the default page size when pageSize is not finite", () => {
    render(<CurtailmentHistory events={mockCurtailmentHistoryEvents} pageSize={Number.NaN} />);

    expect(screen.getByText("Showing 1-4 of 4 curtailment events")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
  });

  it("sorts history rows by target reduction", async () => {
    const user = userEvent.setup();
    render(<CurtailmentHistory events={mockCurtailmentHistoryEvents} pageSize={4} />);

    const targetHeader = screen.getByRole("button", { name: "Target vs actual" });

    expect(targetHeader).toHaveClass("text-emphasis-300");

    await user.click(targetHeader);

    const rows = getRenderedRows();
    expect(within(rows[0]).getByText("Grid peak call")).toBeInTheDocument();
    expect(within(rows[1]).getByText("High price zone")).toBeInTheDocument();
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
    const modal = screen.getByTestId("modal");
    expect(within(modal).getByText("Queued curtailment")).toBeInTheDocument();
    expect(within(modal).getByText("Not started yet")).toBeInTheDocument();

    await user.click(within(modal).getByRole("button", { name: "Stop curtailment" }));

    expect(onStopActiveEvent).toHaveBeenCalledWith(pendingEvent);
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
    const onStopActiveEvent = vi.fn();
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

  it("routes row stop actions through the summary modal", async () => {
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
    const stopButton = within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" });

    expect(screen.queryByRole("button", { name: "View ERCOT ERS obligation" })).not.toBeInTheDocument();
    expect(stopButton).toHaveTextContent("Stop");
    expect(stopButton.querySelector("svg")).toBeNull();

    await user.click(stopButton);

    expect(onStopActiveEvent).not.toHaveBeenCalled();
    const modal = screen.getByTestId("modal");
    expect(within(modal).getByText("Curtailment detail")).toBeInTheDocument();
    expect(within(modal).getByText("ERCOT ERS obligation")).toBeInTheDocument();
    expect(within(modal).getByText("Power target vs actual")).toBeInTheDocument();

    await user.click(within(modal).getByRole("button", { name: "Stop curtailment" }));

    expect(onStopActiveEvent).toHaveBeenCalledWith(mockCurtailmentHistoryEvents[0]);
    expect(within(modal).getByRole("button", { name: "Stop curtailment" })).toBeDisabled();
    expect(within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" })).toBeDisabled();
  });

  it("re-enables stop actions when the stop request fails", async () => {
    const user = userEvent.setup();
    let rejectStopRequest: (error: Error) => void = () => undefined;
    const stopRequest = new Promise<void>((_, reject) => {
      rejectStopRequest = reject;
    });
    const onStopActiveEvent = vi.fn(() => stopRequest);

    render(
      <CurtailmentHistory
        events={mockCurtailmentHistoryEvents}
        activeEventId="curt-1042"
        onStopActiveEvent={onStopActiveEvent}
      />,
    );

    const activeRow = screen.getByTestId("curtailment-history-row-curt-1042");
    await user.click(within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" }));

    const modal = screen.getByTestId("modal");
    const modalStopButton = within(modal).getByRole("button", { name: "Stop curtailment" });

    await user.click(modalStopButton);

    expect(onStopActiveEvent).toHaveBeenCalledWith(mockCurtailmentHistoryEvents[0]);
    expect(modalStopButton).toBeDisabled();
    expect(within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" })).toBeDisabled();

    rejectStopRequest(new Error("Stop request failed"));

    await waitFor(() => expect(modalStopButton).not.toBeDisabled());
    expect(within(activeRow).getByRole("button", { name: "Stop ERCOT ERS obligation" })).not.toBeDisabled();

    await user.click(modalStopButton);

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
    expect(onViewEvent).toHaveBeenCalledWith(mockCurtailmentHistoryEvents[0]);
    expect(screen.getByTestId("modal")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Stop curtailment" }));

    expect(onStopActiveEvent).toHaveBeenCalledWith(mockCurtailmentHistoryEvents[0]);
  });

  it("renders an empty state when there are no events", () => {
    render(<CurtailmentHistory events={[]} />);

    expect(screen.getByText("No results")).toBeInTheDocument();
    expect(screen.queryByTestId("curtailment-history-pagination")).not.toBeInTheDocument();
  });
});
