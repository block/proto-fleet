import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import CurtailmentPill, { type CurtailmentPillEvent } from "./CurtailmentPill";

const triggerName = "View curtailment details for Grid peak call";

const activeCurtailmentEvent: CurtailmentPillEvent = {
  id: "curt-1",
  reason: "Grid peak call",
  state: "active",
  scopeLabel: "Whole org",
  selectedMiners: 48,
  estimatedReductionKw: 126.4,
};

function renderCurtailmentPill({
  event = activeCurtailmentEvent,
  detailsPath,
}: {
  event?: CurtailmentPillEvent;
  detailsPath?: string;
} = {}) {
  return render(
    <MemoryRouter>
      <CurtailmentPill event={event} detailsPath={detailsPath} />
    </MemoryRouter>,
  );
}

function openCurtailmentPopover() {
  fireEvent.click(screen.getByRole("button", { name: triggerName }));
}

describe("CurtailmentPill", () => {
  it("renders the current curtailment state in the trigger", () => {
    renderCurtailmentPill({
      event: {
        ...activeCurtailmentEvent,
        state: "restoring",
      },
    });

    expect(screen.getByRole("button", { name: triggerName })).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByText("Curtailment restoring")).toBeVisible();
  });

  it("shows curtailment details in the popover", () => {
    renderCurtailmentPill();

    openCurtailmentPopover();

    expect(screen.getByText("Grid peak call")).toBeInTheDocument();
    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(screen.getByText("Whole org")).toBeInTheDocument();
    expect(screen.getByText("48 selected miners - 126.4 kW planned")).toBeInTheDocument();
  });

  it("formats singular miner counts", () => {
    renderCurtailmentPill({
      event: {
        ...activeCurtailmentEvent,
        selectedMiners: 1,
        estimatedReductionKw: 4,
      },
    });

    openCurtailmentPopover();

    expect(screen.getByText("1 selected miner - 4.0 kW planned")).toBeInTheDocument();
  });

  it("renders the details link only when a details path is provided", () => {
    const { unmount } = renderCurtailmentPill();

    openCurtailmentPopover();

    expect(screen.queryByText("View curtailment")).not.toBeInTheDocument();

    unmount();
    render(
      <MemoryRouter>
        <CurtailmentPill event={activeCurtailmentEvent} detailsPath="/energy" />
      </MemoryRouter>,
    );

    openCurtailmentPopover();

    expect(screen.getByText("View curtailment").closest("a")).toHaveAttribute("href", "/energy");
  });
});
