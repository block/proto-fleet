import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import type { RolloutLane } from "../rolloutTypes";
import LaneFirmwareConvergenceStatus from "./LaneFirmwareConvergenceStatus";

function lane(overrides: Partial<RolloutLane["firmwareConvergence"]> = {}): RolloutLane {
  return {
    id: "lane-1",
    label: "Stable production",
    description: "",
    currentChannelId: 41n,
    revision: 1n,
    channels: [],
    memberCount: 2,
    memberIdentifiers: [],
    currentReleaseTargets: [],
    createdAt: "2026-08-18T12:00:00.000Z",
    firmwareConvergence: {
      totalCount: 2,
      pendingCount: 0,
      updatingCount: 0,
      verifyingCount: 0,
      confirmedCount: 2,
      attentionCount: 0,
      members: [],
      ...overrides,
    },
  };
}

describe("LaneFirmwareConvergenceStatus", () => {
  it("uses the normal firmware rollout status lockup and progress", () => {
    render(
      <LaneFirmwareConvergenceStatus
        lane={lane({
          confirmedCount: 1,
          updatingCount: 1,
          members: [
            {
              deviceIdentifier: "miner-1",
              manufacturer: "Proto",
              model: "Alpha",
              latestObservedFirmwareVersion: "2.0.0",
              targetFirmwareVersion: "2.0.0",
              state: "confirmed",
            },
            {
              deviceIdentifier: "miner-2",
              manufacturer: "Proto",
              model: "Alpha",
              latestObservedFirmwareVersion: "1.0.0",
              targetFirmwareVersion: "2.0.0",
              state: "updating",
            },
          ],
        })}
        canStart
        onStart={vi.fn()}
      />,
    );

    expect(screen.getByTestId("active-rollout-primary-lockup")).toHaveTextContent("Update status");
    expect(screen.getByTestId("active-rollout-primary-lockup")).toHaveTextContent("Updating");
    expect(screen.getByTestId("active-rollout-progress")).toHaveTextContent("1 of 2 miners updated (50%)");
    expect(screen.getByTestId("active-rollout-convergence-progress")).toHaveTextContent("1 of 2");
    expect(screen.getByRole("progressbar", { name: "Firmware convergence" })).toHaveAttribute("aria-valuenow", "1");
    expect(screen.queryByRole("button", { name: /pause|abort|retry|force/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Close status" })).not.toBeInTheDocument();
  });

  it("shows Lane ready and keeps terminal details closable", async () => {
    const user = userEvent.setup();
    const onStart = vi.fn();
    const onClose = vi.fn();
    render(<LaneFirmwareConvergenceStatus lane={lane()} canStart onClose={onClose} onStart={onStart} />);

    expect(screen.getByText("Lane ready")).toBeInTheDocument();
    expect(screen.getByTestId("active-rollout-primary-lockup")).toHaveTextContent("Completed");
    await user.click(screen.getByRole("button", { name: "Start rollout" }));
    await user.click(screen.getByRole("button", { name: "Close status" }));

    expect(onStart).toHaveBeenCalledOnce();
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("uses the normal error presentation without recovery controls", () => {
    render(
      <LaneFirmwareConvergenceStatus
        lane={lane({
          confirmedCount: 1,
          attentionCount: 1,
          members: [
            {
              deviceIdentifier: "miner-2",
              manufacturer: "Proto",
              model: "Alpha",
              latestObservedFirmwareVersion: "1.0.0",
              targetFirmwareVersion: "2.0.0",
              state: "needsAttention",
              lastError: "Firmware identity could not be confirmed",
            },
          ],
        })}
        canStart
        onClose={vi.fn()}
        onStart={vi.fn()}
      />,
    );

    expect(screen.getByTestId("active-rollout-primary-lockup")).toHaveTextContent("Completed with failures");
    expect(screen.getByTestId("active-rollout-error-banner")).toHaveTextContent("1 error affecting 1 miner");
    expect(screen.getByText("Review impacted miners before continuing.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Start rollout" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /retry|force/i })).not.toBeInTheDocument();
  });
});
