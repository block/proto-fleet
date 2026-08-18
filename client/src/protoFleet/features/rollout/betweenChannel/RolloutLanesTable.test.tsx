import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import RolloutLanesTable, { type LaneTableRow } from "./RolloutLanesTable";
import type { RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";

const rows: LaneTableRow[] = [
  {
    id: "lane-1",
    lane: {
      id: "lane-1",
      label: "Stable production",
      description: "Production firmware",
      currentChannelId: 41n,
      revision: 2n,
      channels: [],
      memberCount: 12,
      memberIdentifiers: [],
      initialEnforcement: {
        totalCount: 12,
        pendingCount: 0,
        updatingCount: 0,
        verifyingCount: 0,
        confirmedCount: 12,
        attentionCount: 0,
        members: [],
      },
      currentReleaseTargets: [
        {
          firmwareFileId: "alpha-1",
          targetManufacturer: "Proto",
          targetModel: "Alpha",
          firmwareVersion: "1.0.0",
          sha256: "a",
        },
      ],
    },
  },
];

const abortedSplit: RolloutRecord = {
  id: "rollout-1",
  name: "Production 2.0.0",
  strategyKey: "between_channel",
  state: "aborted",
  revision: 2n,
  sourceChannelId: 41n,
  targetChannelId: 42n,
  reason: "Operator aborted",
  batches: [],
  members: [
    {
      id: 1n,
      batchId: 1n,
      deviceIdentifier: "miner-1",
      position: 0,
      state: "succeeded",
      revision: 1n,
      evidence: [],
    },
  ],
  causes: [],
  availableActions: {
    admit: false,
    continue: false,
    pause: false,
    resume: false,
    abort: false,
    revert: true,
    complete: false,
  },
};

describe("RolloutLanesTable", () => {
  it("shows stable lane release and membership to read-only operators", () => {
    render(<RolloutLanesTable rows={rows} canStart={false} onSetup={vi.fn()} onStart={vi.fn()} onView={vi.fn()} />);

    expect(screen.getByText("Stable production")).toBeInTheDocument();
    expect(screen.getByText("Alpha 1.0.0")).toBeInTheDocument();
    expect(screen.getByText("12")).toBeInTheDocument();
    expect(screen.getByText("12 confirmed")).toBeInTheDocument();
    expect(screen.getByText("0 needs attention")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /start rollout/i })).not.toBeInTheDocument();
  });

  it("shows lane start only to managers", () => {
    render(<RolloutLanesTable rows={rows} canStart onSetup={vi.fn()} onStart={vi.fn()} onView={vi.fn()} />);

    expect(screen.getByRole("button", { name: "Start rollout for Stable production" })).toBeEnabled();
  });

  it("shows destructive deletion only to channel managers", () => {
    const onDelete = vi.fn();
    const { rerender } = render(
      <RolloutLanesTable
        rows={rows}
        canStart={false}
        canDelete={false}
        onSetup={vi.fn()}
        onStart={vi.fn()}
        onView={vi.fn()}
        onDelete={onDelete}
      />,
    );
    expect(screen.queryByRole("button", { name: "Delete Stable production" })).not.toBeInTheDocument();

    rerender(
      <RolloutLanesTable
        rows={rows}
        canStart={false}
        canDelete
        onSetup={vi.fn()}
        onStart={vi.fn()}
        onView={vi.fn()}
        onDelete={onDelete}
      />,
    );
    screen.getByRole("button", { name: "Delete Stable production" }).click();
    expect(onDelete).toHaveBeenCalledWith(rows[0].lane);
  });

  it("disables deletion with a concise reason while rollout work is active", () => {
    render(
      <RolloutLanesTable
        rows={[
          {
            ...rows[0],
            latestRollout: {
              ...abortedSplit,
              state: "running",
              members: [{ ...abortedSplit.members[0], state: "admitted" }],
            },
          },
        ]}
        canStart={false}
        canDelete
        onSetup={vi.fn()}
        onStart={vi.fn()}
        onView={vi.fn()}
        onDelete={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "Delete Stable production" })).toBeDisabled();
    expect(screen.getByText("Wait for rollout work to settle before deleting this lane.")).toBeInTheDocument();
  });

  it("disables deletion when rollout visibility cannot establish safety", () => {
    render(
      <RolloutLanesTable
        rows={rows}
        canStart={false}
        canDelete
        deletePermissionBlockedReason="Rollout read access is required to verify this lane is safe to delete."
        onSetup={vi.fn()}
        onStart={vi.fn()}
        onView={vi.fn()}
        onDelete={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "Delete Stable production" })).toBeDisabled();
    expect(
      screen.getByText("Rollout read access is required to verify this lane is safe to delete."),
    ).toBeInTheDocument();
  });

  it("lets an operator reopen initial firmware setup from the lane row", async () => {
    const onSetup = vi.fn();
    render(<RolloutLanesTable rows={rows} canStart={false} onSetup={onSetup} onStart={vi.fn()} onView={vi.fn()} />);

    screen.getByRole("button", { name: "View initial firmware setup for Stable production" }).click();

    expect(onSetup).toHaveBeenCalledWith(rows[0].lane);
  });

  it("disables start and explains how to resolve an aborted lane split", () => {
    render(
      <RolloutLanesTable
        rows={[{ ...rows[0], latestRollout: abortedSplit }]}
        canStart
        onSetup={vi.fn()}
        onStart={vi.fn()}
        onView={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "Start rollout for Stable production" })).toBeDisabled();
    expect(screen.getByText(/Revert or resolve miners left on a historical release/i)).toBeInTheDocument();
  });

  it("disables every start action while fresh lane preparation is in progress", () => {
    render(
      <RolloutLanesTable rows={rows} canStart isPreparingStart onSetup={vi.fn()} onStart={vi.fn()} onView={vi.fn()} />,
    );

    expect(screen.getByRole("button", { name: "Start rollout for Stable production" })).toBeDisabled();
  });

  it("blocks rollout start while initial firmware is still converging", () => {
    const activeLane = {
      ...rows[0].lane,
      initialEnforcement: {
        totalCount: 12,
        pendingCount: 2,
        updatingCount: 3,
        verifyingCount: 1,
        confirmedCount: 6,
        attentionCount: 0,
        members: [],
      },
    };

    render(
      <RolloutLanesTable
        rows={[{ id: activeLane.id, lane: activeLane }]}
        canStart
        onSetup={vi.fn()}
        onStart={vi.fn()}
        onView={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "Start rollout for Stable production" })).toBeDisabled();
    expect(
      screen.getByText("Wait for initial firmware setup to finish before starting a rollout."),
    ).toBeInTheDocument();
  });

  it("blocks rollout start until every initial member is confirmed", () => {
    const attentionLane = {
      ...rows[0].lane,
      initialEnforcement: {
        ...rows[0].lane.initialEnforcement,
        confirmedCount: 11,
        attentionCount: 1,
      },
    };

    render(
      <RolloutLanesTable
        rows={[{ id: attentionLane.id, lane: attentionLane }]}
        canStart
        onSetup={vi.fn()}
        onStart={vi.fn()}
        onView={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "Start rollout for Stable production" })).toBeDisabled();
    expect(screen.getByText("Resolve miners that need attention before starting a rollout.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /retry/i })).not.toBeInTheDocument();
  });
});
