import { describe, expect, it } from "vitest";

import {
  buildManualBatches,
  canCompleteWithFailures,
  canRevertRollout,
  evaluateTargetCompatibility,
  rolloutLaneStartBlockedReason,
  shouldMonitorRollout,
} from "./betweenChannelUtils";
import { minerTargetKey } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";
import type {
  RolloutLane,
  RolloutLaneReleaseTarget,
  RolloutMemberState,
  RolloutRecord,
} from "@/protoFleet/features/rollout/rolloutTypes";

const sourceTargets: RolloutLaneReleaseTarget[] = [
  {
    firmwareFileId: "alpha-1",
    targetManufacturer: "Proto",
    targetModel: "Alpha",
    firmwareVersion: "1.0.0",
    sha256: "a",
  },
  {
    firmwareFileId: "beta-1",
    targetManufacturer: "Proto",
    targetModel: "Beta",
    firmwareVersion: "2.0.0",
    sha256: "b",
  },
];

const files = [
  {
    id: "alpha-1",
    filename: "alpha-1.swu",
    size: 100,
    uploaded_at: "2026-08-18T00:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Alpha",
    firmware_version: "1.0.0",
  },
  {
    id: "alpha-2",
    filename: "alpha-2.swu",
    size: 100,
    uploaded_at: "2026-08-18T00:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Alpha",
    firmware_version: "2.0.0",
  },
  {
    id: "beta-3",
    filename: "beta-3.swu",
    size: 100,
    uploaded_at: "2026-08-18T00:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Beta",
    firmware_version: "3.0.0",
  },
];

function targetKey(manufacturer: string, model: string): string {
  const key = minerTargetKey(manufacturer, model);
  if (key === null) {
    throw new Error("Test target must be complete.");
  }
  return key;
}

function rolloutWithMembers(
  state: RolloutRecord["state"],
  memberStates: RolloutMemberState[],
  overrides: Partial<RolloutRecord> = {},
): RolloutRecord {
  return {
    id: "rollout-1",
    name: "Production rollout",
    strategyKey: "between_channel",
    state,
    revision: 1n,
    sourceChannelId: 41n,
    targetChannelId: 42n,
    reason: "Validated release",
    batches: [
      {
        id: 1n,
        position: 0,
        label: "Final",
        state: "completed",
        revision: 1n,
        members: [],
      },
    ],
    members: memberStates.map((memberState, index) => ({
      id: BigInt(index + 1),
      batchId: 1n,
      deviceIdentifier: `miner-${index + 1}`,
      position: index,
      state: memberState,
      revision: 1n,
      evidence: [],
    })),
    causes: [],
    availableActions: {
      admit: false,
      continue: false,
      pause: false,
      resume: false,
      abort: false,
      revert: state === "aborted",
      complete: state === "review",
    },
    ...overrides,
  };
}

const lane: RolloutLane = {
  id: "lane-1",
  label: "Stable production",
  description: "",
  currentChannelId: 41n,
  revision: 1n,
  channels: [],
  memberCount: 2,
  memberIdentifiers: [],
  currentReleaseTargets: [],
  initialEnforcement: {
    totalCount: 2,
    pendingCount: 0,
    updatingCount: 0,
    verifyingCount: 0,
    confirmedCount: 2,
    attentionCount: 0,
    members: [],
  },
};

describe("between-channel rollout helpers", () => {
  it("blocks a target that is missing a source model", () => {
    const rows = evaluateTargetCompatibility(sourceTargets, files, {
      [targetKey("Proto", "Alpha")]: "alpha-2",
    });

    expect(rows).toEqual([
      expect.objectContaining({ model: "Alpha", status: "compatible" }),
      expect.objectContaining({ model: "Beta", status: "missing" }),
    ]);
  });

  it("blocks a no-op target release", () => {
    const rows = evaluateTargetCompatibility(sourceTargets, files, {
      [targetKey("Proto", "Alpha")]: "alpha-1",
      [targetKey("Proto", "Beta")]: "beta-3",
    });

    expect(rows[0]).toMatchObject({
      sourceVersion: "1.0.0",
      targetVersion: "1.0.0",
      status: "noOp",
    });
  });

  it("builds deterministic pilot and manual batch assignments", () => {
    const members = ["miner-3", "miner-1", "miner-2", "miner-4"];

    expect(buildManualBatches(members, { strategy: "pilotThenContinue", pilotSize: 1 })).toEqual([
      { label: "Pilot", members: [{ deviceIdentifier: "miner-3" }] },
      {
        label: "Remaining",
        members: [{ deviceIdentifier: "miner-1" }, { deviceIdentifier: "miner-2" }, { deviceIdentifier: "miner-4" }],
      },
    ]);
    expect(buildManualBatches(members, { strategy: "batched", batchSize: 2 })).toEqual([
      {
        label: "Batch 1",
        members: [{ deviceIdentifier: "miner-3" }, { deviceIdentifier: "miner-1" }],
      },
      {
        label: "Batch 2",
        members: [{ deviceIdentifier: "miner-2" }, { deviceIdentifier: "miner-4" }],
      },
    ]);
  });

  it("keeps aborted rollouts live until every member settles before exposing revert", () => {
    const unsettled = rolloutWithMembers("aborted", ["succeeded", "admitted"]);
    const settled = rolloutWithMembers("aborted", ["succeeded", "cancelled"]);

    expect(shouldMonitorRollout(unsettled)).toBe(true);
    expect(canRevertRollout(unsettled)).toBe(false);
    expect(shouldMonitorRollout(settled)).toBe(false);
    expect(canRevertRollout(settled)).toBe(true);
  });

  it("blocks a new rollout while members remain on a non-current lane channel", () => {
    const abortedSplit = rolloutWithMembers("aborted", ["succeeded", "cancelled"]);
    const completedWithFailures = rolloutWithMembers("completedWithFailures", ["succeeded", "failed"]);

    expect(rolloutLaneStartBlockedReason(lane, abortedSplit)).toMatch(/Revert or resolve/i);
    expect(
      rolloutLaneStartBlockedReason(
        {
          ...lane,
          currentChannelId: 42n,
        },
        completedWithFailures,
      ),
    ).toMatch(/Revert or resolve/i);
    expect(
      rolloutLaneStartBlockedReason({ ...lane, currentChannelId: 42n }, rolloutWithMembers("completed", ["succeeded"])),
    ).toBeNull();
  });

  it("offers explicit failure completion only after the final batch settles in review", () => {
    const finalFailure = rolloutWithMembers("review", ["succeeded", "attentionRequired"]);
    const pendingBatch = rolloutWithMembers("review", ["succeeded", "failed"], {
      batches: [
        {
          id: 1n,
          position: 0,
          label: "Remaining",
          state: "pending",
          revision: 1n,
          members: [],
        },
      ],
    });

    expect(canCompleteWithFailures(finalFailure)).toBe(true);
    expect(canCompleteWithFailures(pendingBatch)).toBe(false);
    expect(canCompleteWithFailures(rolloutWithMembers("review", ["succeeded"]))).toBe(false);
  });
});
