import { describe, expect, it } from "vitest";

import {
  BETWEEN_CHANNEL_STRATEGY_KEY,
  buildManualBatches,
  canCompleteWithFailures,
  canRevertRollout,
  compareRolloutChildren,
  dominantFirmwareConvergenceState,
  evaluateTargetCompatibility,
  firstActiveFirmwareConvergenceLane,
  laneForRollout,
  rolloutLaneActionStatus,
  rolloutLaneDeleteBlockedReason,
  rolloutLaneMembershipBlockedReason,
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
    strategyKey: BETWEEN_CHANNEL_STRATEGY_KEY,
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
  firmwareConvergence: {
    totalCount: 2,
    pendingCount: 0,
    updatingCount: 0,
    verifyingCount: 0,
    confirmedCount: 2,
    attentionCount: 0,
    members: [],
  },
  models: [],
  scalarProjectionAvailable: true,
  topologyEnabled: false,
};

describe("between-channel rollout helpers", () => {
  it("selects lanes in the server-provided order", () => {
    const inactiveLane = { ...lane, id: "inactive" };
    const firstActiveLane = {
      ...lane,
      id: "first-active",
      firmwareConvergence: { ...lane.firmwareConvergence, confirmedCount: 1, updatingCount: 1 },
    };
    const secondActiveLane = {
      ...firstActiveLane,
      id: "second-active",
    };
    const rolloutLane = {
      ...lane,
      id: "rollout-lane",
      channels: [{ channelId: 42n, releaseSetId: 8n, position: 1, rolloutId: "rollout-1", current: true }],
    };
    const lanes = [inactiveLane, firstActiveLane, secondActiveLane, rolloutLane];

    expect(firstActiveFirmwareConvergenceLane(lanes)).toBe(firstActiveLane);
    expect(laneForRollout(lanes, "rollout-1")).toBe(rolloutLane);
  });

  it("orders model children by action required, active work, terminal history, then model label", () => {
    const terminal = rolloutWithMembers("completed", ["succeeded"], {
      id: "terminal",
      manufacturer: "Proto",
      model: "Alpha",
    });
    const active = rolloutWithMembers("running", ["admitted"], {
      id: "active",
      manufacturer: "Proto",
      model: "Beta",
    });
    const reviewZulu = rolloutWithMembers("review", ["succeeded"], {
      id: "review-zulu",
      manufacturer: "Proto",
      model: "Zulu",
    });
    const reviewAlpha = rolloutWithMembers("review", ["succeeded"], {
      id: "review-alpha",
      manufacturer: "Antminer",
      model: "S21",
    });

    expect([terminal, reviewZulu, active, reviewAlpha].sort(compareRolloutChildren).map(({ id }) => id)).toEqual([
      "review-alpha",
      "review-zulu",
      "active",
      "terminal",
    ]);
  });

  it.each([
    ["needsAttention", { attentionCount: 1, verifyingCount: 1, updatingCount: 1, pendingCount: 1 }],
    ["verifying", { attentionCount: 0, verifyingCount: 1, updatingCount: 1, pendingCount: 1 }],
    ["updating", { attentionCount: 0, verifyingCount: 0, updatingCount: 1, pendingCount: 1 }],
    ["pending", { attentionCount: 0, verifyingCount: 0, updatingCount: 0, pendingCount: 1 }],
    ["confirmed", { attentionCount: 0, verifyingCount: 0, updatingCount: 0, pendingCount: 0 }],
  ] as const)("returns %s as the dominant firmware convergence state", (expected, counts) => {
    expect(
      dominantFirmwareConvergenceState({
        ...lane,
        firmwareConvergence: {
          ...lane.firmwareConvergence,
          ...counts,
        },
      }),
    ).toBe(expected);
  });

  it("returns one concise action status for visible lane actions", () => {
    const activeInitialLane = {
      ...lane,
      firmwareConvergence: { ...lane.firmwareConvergence, confirmedCount: 1, updatingCount: 1 },
    };
    const activeRollout = rolloutWithMembers("running", ["admitted"]);

    expect(
      rolloutLaneActionStatus(activeInitialLane, undefined, {
        canStart: true,
        canDelete: true,
      }),
    ).toBe("Firmware convergence in progress.");
    expect(
      rolloutLaneActionStatus(lane, activeRollout, {
        canStart: true,
        canDelete: true,
      }),
    ).toBe("Rollout in progress.");
    expect(
      rolloutLaneActionStatus(lane, undefined, {
        canStart: false,
        canDelete: true,
        deletePermissionBlockedReason: "Rollout read access is required.",
      }),
    ).toBe("Rollout read access is required.");
    expect(
      rolloutLaneActionStatus(lane, undefined, {
        canStart: false,
        canDelete: false,
      }),
    ).toBeNull();
  });

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

  it("keeps completed rollouts live until the latest completed batch evidence is finalized", () => {
    const unfinalized = rolloutWithMembers("completed", ["succeeded"], {
      batches: [
        {
          id: 1n,
          position: 0,
          label: "Pilot",
          state: "completed",
          revision: 1n,
          members: [],
          evidenceSummary: {
            status: "finalized",
            totalCount: 1n,
            pairedCount: 1n,
            postWindowFinalized: true,
          },
        },
        {
          id: 2n,
          position: 1,
          label: "Final",
          state: "completed",
          revision: 1n,
          members: [],
          completedAt: "2026-08-18T02:00:00.000Z",
          evidenceSummary: {
            status: "observing",
            totalCount: 1n,
            pairedCount: 1n,
            postWindowFinalized: false,
          },
        },
      ],
    });

    expect(shouldMonitorRollout(unfinalized)).toBe(true);
    expect(
      shouldMonitorRollout({
        ...unfinalized,
        batches: unfinalized.batches.map((batch) =>
          batch.id === 2n
            ? {
                ...batch,
                evidenceSummary: {
                  ...batch.evidenceSummary!,
                  status: "finalized",
                  postWindowFinalized: true,
                },
              }
            : batch,
        ),
      }),
    ).toBe(false);
    expect(shouldMonitorRollout(rolloutWithMembers("completed", ["succeeded"]))).toBe(false);
  });

  it("does not monitor legacy completed batches without a completion timestamp", () => {
    const legacy = rolloutWithMembers("completed", ["succeeded"], {
      batches: [
        {
          id: 1n,
          position: 0,
          label: "Legacy final",
          state: "completed",
          revision: 1n,
          members: [],
          evidenceSummary: {
            status: "pending",
            totalCount: 0n,
            pairedCount: 0n,
            postWindowFinalized: false,
          },
        },
      ],
    });

    expect(shouldMonitorRollout(legacy)).toBe(false);
  });

  it("blocks lane deletion only while setup or rollout work is unsettled", () => {
    const activeInitialLane = {
      ...lane,
      firmwareConvergence: {
        ...lane.firmwareConvergence,
        pendingCount: 1,
        confirmedCount: 1,
      },
    };

    expect(rolloutLaneDeleteBlockedReason(activeInitialLane, undefined)).toMatch(/firmware convergence/i);
    expect(rolloutLaneDeleteBlockedReason(lane, rolloutWithMembers("running", ["admitted"]))).toMatch(/rollout work/i);
    expect(rolloutLaneDeleteBlockedReason(lane, rolloutWithMembers("aborted", ["admitted"]))).toMatch(/rollout work/i);
    expect(rolloutLaneDeleteBlockedReason(lane, rolloutWithMembers("aborted", ["succeeded", "cancelled"]))).toBeNull();
    expect(
      rolloutLaneDeleteBlockedReason(
        {
          ...lane,
          firmwareConvergence: {
            ...lane.firmwareConvergence,
            confirmedCount: 1,
            attentionCount: 1,
          },
        },
        undefined,
      ),
    ).toBeNull();
  });

  it("blocks membership changes while firmware enforcement or rollout work is active", () => {
    const activeMembershipEnforcement = {
      ...lane,
      firmwareConvergence: {
        ...lane.firmwareConvergence,
        pendingCount: 1,
        confirmedCount: 1,
      },
    };

    expect(rolloutLaneMembershipBlockedReason(activeMembershipEnforcement, undefined)).toMatch(
      /firmware updates to finish/i,
    );
    expect(rolloutLaneMembershipBlockedReason(lane, rolloutWithMembers("running", ["admitted"]))).toMatch(
      /rollout work to settle/i,
    );
    expect(rolloutLaneMembershipBlockedReason(lane, rolloutWithMembers("aborted", ["admitted"]))).toMatch(
      /rollout work to settle/i,
    );
    expect(rolloutLaneMembershipBlockedReason(lane, rolloutWithMembers("completed", ["succeeded"]))).toBeNull();
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

  it("directs empty lanes to add miners before starting", () => {
    expect(
      rolloutLaneStartBlockedReason(
        {
          ...lane,
          memberCount: 0,
          firmwareConvergence: {
            ...lane.firmwareConvergence,
            totalCount: 0,
            confirmedCount: 0,
          },
        },
        undefined,
      ),
    ).toBe("Add miners before starting a rollout.");
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
