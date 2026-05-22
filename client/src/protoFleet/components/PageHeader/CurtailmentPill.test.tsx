import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import CurtailmentPill, { type CurtailmentPillEvent } from "./CurtailmentPill";

const triggerName = "View curtailment details for Grid peak call";

const activeCurtailmentEvent: CurtailmentPillEvent = {
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

function openCurtailmentPopover(): void {
  fireEvent.click(screen.getByRole("button", { name: triggerName }));
}

function getPlannedReductionText(selectedMiners: number, estimatedReductionKw: number): string {
  const minerLabel = selectedMiners === 1 ? "miner" : "miners";
  const selectedMinersText = `${selectedMiners.toLocaleString()} selected ${minerLabel}`;
  const estimatedReductionText = `${estimatedReductionKw.toLocaleString(undefined, {
    maximumFractionDigits: 1,
    minimumFractionDigits: 1,
  })} kW`;

  return `${selectedMinersText} - ${estimatedReductionText} planned`;
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
    expect(screen.getByText(getPlannedReductionText(48, 126.4))).toBeInTheDocument();
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

    expect(screen.getByText(getPlannedReductionText(1, 4))).toBeInTheDocument();
  });

  it("does not render the details link without a details path", () => {
    renderCurtailmentPill();

    openCurtailmentPopover();

    expect(screen.queryByText("View curtailment")).not.toBeInTheDocument();
  });

  it("links to the provided details path", () => {
    renderCurtailmentPill({ detailsPath: "/energy" });

    openCurtailmentPopover();

    expect(screen.getByText("View curtailment")).toHaveAttribute("href", "/energy");
  });
});
