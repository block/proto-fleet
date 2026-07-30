import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import CurtailmentPill, { type CurtailmentPillEvent } from "./CurtailmentPill";

const triggerName = "View curtailment details for Grid peak call";

const activeCurtailmentEvent: CurtailmentPillEvent = {
  reason: "Grid peak call",
  state: "curtailing",
  scopeLabel: "Whole org",
  selectedMiners: 48,
  estimatedReductionKw: 126.4,
  targetMetricsAvailable: true,
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
  it.each([
    ["pending", "Curtailment pending", "bg-core-accent-fill"],
    ["curtailing", "Curtailment active", "bg-intent-warning-fill"],
    ["curtailed", "Curtailment active", "bg-intent-warning-fill"],
    ["restoring", "Curtailment restoring", "bg-core-accent-fill"],
  ] as const)("renders the legacy %s curtailment state in the trigger", (state, label, dotClassName) => {
    renderCurtailmentPill({ event: { ...activeCurtailmentEvent, state } });

    const trigger = screen.getByRole("button", { name: triggerName });
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(trigger.querySelector(`.${dotClassName}`)).not.toBeNull();
    expect(screen.getByText(label)).toBeVisible();
  });

  it("shows curtailment details in the popover", () => {
    renderCurtailmentPill();

    openCurtailmentPopover();

    expect(screen.getByText("Grid peak call")).toBeInTheDocument();
    expect(screen.getByText("Curtailing")).toBeInTheDocument();
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

  it("shows unavailable target metrics without zero values", () => {
    renderCurtailmentPill({
      event: {
        ...activeCurtailmentEvent,
        selectedMiners: 0,
        estimatedReductionKw: 0,
        targetMetricsAvailable: false,
      },
    });

    openCurtailmentPopover();

    expect(screen.getByText("Target details unavailable")).toBeInTheDocument();
    expect(screen.queryByText(getPlannedReductionText(0, 0))).not.toBeInTheDocument();
  });

  it("shows the live miner count without a fabricated estimate when only counts are available", () => {
    renderCurtailmentPill({
      event: {
        ...activeCurtailmentEvent,
        selectedMiners: 5000,
        estimatedReductionKw: 0,
        targetMetricsAvailable: true,
        estimatedReductionAvailable: false,
      },
    });

    openCurtailmentPopover();

    expect(screen.getByText("5,000 selected miners")).toBeInTheDocument();
    expect(screen.queryByText(getPlannedReductionText(5000, 0))).not.toBeInTheDocument();
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
