import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import userEvent from "@testing-library/user-event";

import RolloutPill from "./RolloutPill";
import type { RolloutEvent } from "./rolloutTypes";

const event: RolloutEvent = {
  processType: "firmware",
  state: "inProgress",
  title: "Firmware convergence",
  scopeLabel: "Stable production",
  strategy: "allAtOnce",
  order: "random",
  totalTargets: 6,
  excludedTargets: 0,
  convergenceProgress: {
    completed: 2,
    total: 6,
    attentionRequired: 0,
  },
  rollups: [
    { phase: "done", count: 2 },
    { phase: "inProgress", count: 1 },
    { phase: "queued", count: 3 },
  ],
};

describe("RolloutPill", () => {
  it("shows concise firmware status and links to the active header detail", async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <RolloutPill event={event} detailsPath="/settings/firmware?tab=rolloutLanes&setupLane=lane-1" />
      </MemoryRouter>,
    );

    await user.click(screen.getByRole("button", { name: "View update details for Firmware convergence" }));

    expect(screen.getByText("Firmware update in progress")).toBeInTheDocument();
    expect(screen.getByText("Stable production")).toBeInTheDocument();
    expect(screen.getByText("View update").closest("a")).toHaveAttribute(
      "href",
      "/settings/firmware?tab=rolloutLanes&setupLane=lane-1",
    );
  });
});
