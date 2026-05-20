import { render, screen, within } from "@testing-library/react";
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

  it("opens the summary modal from row click and stops active events from the action button", async () => {
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

    expect(onStopActiveEvent).toHaveBeenCalledWith(mockCurtailmentHistoryEvents[0]);
    expect(screen.queryByTestId("modal")).not.toBeInTheDocument();

    await user.click(activeRow);

    const modal = screen.getByTestId("modal");
    expect(within(modal).getByText("Curtailment detail")).toBeInTheDocument();
    expect(within(modal).getByText("ERCOT ERS obligation")).toBeInTheDocument();
    expect(within(modal).getByText("Power target vs actual")).toBeInTheDocument();
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

    expect(onStopActiveEvent).toHaveBeenCalledWith(mockCurtailmentHistoryEvents[0]);
    expect(onViewEvent).not.toHaveBeenCalled();
    expect(screen.queryByTestId("modal")).not.toBeInTheDocument();
  });

  it("renders an empty state when there are no events", () => {
    render(<CurtailmentHistory events={[]} />);

    expect(screen.getByText("No results")).toBeInTheDocument();
    expect(screen.queryByTestId("curtailment-history-pagination")).not.toBeInTheDocument();
  });
});
