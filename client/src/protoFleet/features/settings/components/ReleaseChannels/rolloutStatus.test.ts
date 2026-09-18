import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";

import { planReadout } from "./behaviorUtils";
import {
  activeRigRollout,
  batchedAutoBehavior,
  batchedRigRollout,
  canaryChannel,
  canceledRemainingRigRollout,
  completedRigRollout,
  completedWithFailuresRigRollout,
  gatedRigRollout,
  pausedRigRollout,
  pilotBehavior,
  rolloutDevices,
  singleBatchBehavior,
} from "./ReleaseChannels.fixtures";
import {
  activeRolloutForGroup,
  activeUpdateSummary,
  canRetryFailed,
  channelAssignmentKey,
  channelUpdateStatus,
  deviceCounts,
  evidenceScopeLabel,
  failedDevices,
  hasUnavailableAssignedFirmware,
  lastFinishedByChannelAssignment,
  metricDisplay,
  modelFirmwareLabel,
  modelUpdateStatus,
  pacingSummary,
  pairKey,
  rolloutDeviceCounts,
  rolloutNeedsAttention,
  rolloutOutcomeLabel,
  rolloutProgressSegments,
  rolloutProgressSummary,
  rolloutStageLabel,
  scopeCounts,
  scopeDevices,
  scopedToBatch,
} from "./rolloutStatus";

import { isScopeEmpty, scopeSummary } from "./scopeUtils";
import {
  ReleaseChannelScopeSchema,
  RolloutBehaviorSchema,
  RolloutDeviceCountsSchema,
  RolloutDevicePhase,
  RolloutDeviceSchema,
  RolloutEvidenceSchema,
  RolloutMethod,
  RolloutSchema,
  RolloutStage,
  RolloutState,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { gatesAfterBatch } from "@/protoFleet/api/rolloutBehavior";

describe("release channel target keys", () => {
  it("isolates missing identity halves from each other and from observed controls", () => {
    const pairs = [
      { manufacturer: "", model: "Rig" },
      { manufacturer: "Rig", model: "" },
      { manufacturer: "\u0000", model: "Rig" },
      { manufacturer: "a\u0000b", model: "c" },
      { manufacturer: "a", model: "b\u0000c" },
    ];
    expect(new Set(pairs.map(pairKey)).size).toBe(pairs.length);
  });
});
const rigGroup = { ...canaryChannel.modelGroups[0], activeRolloutId: 0n, assignmentGeneration: 1n };

describe("active rollout identity", () => {
  const group = {
    ...rigGroup,
    activeRolloutId: activeRigRollout.id,
    assignmentGeneration: activeRigRollout.assignmentGeneration,
  };
  it.each([
    { channelId: 99n },
    { manufacturer: "Other" },
    { model: "Other" },
    { assignmentGeneration: 99n },
    { status: RolloutStatus.COMPLETED },
  ])("does not attach an inconsistent rollout summary (%#)", (patch) => {
    const rollout = { ...activeRigRollout, ...patch };
    expect(activeRolloutForGroup(1n, group, new Map([[rollout.id, rollout]]))).toBeUndefined();
  });
  it("joins normalized observed aliases only to the reported active ID", () => {
    const rollouts = new Map([
      [activeRigRollout.id, activeRigRollout],
      [gatedRigRollout.id, gatedRigRollout],
    ]);
    expect(activeRolloutForGroup(1n, { ...group, manufacturer: " proto ", model: " rig " }, rollouts)).toBe(
      activeRigRollout,
    );
    expect(activeRolloutForGroup(1n, { ...group, activeRolloutId: 0n }, rollouts)).toBeUndefined();
    expect(activeRolloutForGroup(1n, { ...group, activeRolloutId: 999n }, rollouts)).toBeUndefined();
  });
});

describe("failed rollout retry eligibility", () => {
  const rollout = completedWithFailuresRigRollout;
  const group = {
    ...rigGroup,
    assignmentGeneration: rollout.assignmentGeneration,
    firmwareChecksum: rollout.firmwareChecksum,
    activeRolloutId: 0n,
  };
  const channel = { ...canaryChannel, modelGroups: [group] };

  it("allows active retries without requiring the channel snapshot", () => {
    expect(canRetryFailed(batchedRigRollout, [], [batchedRigRollout])).toBe(true);
    expect(canRetryFailed(activeRigRollout, [], [activeRigRollout])).toBe(false);
  });

  it("allows a finished retry of the current assignment, including normalized observed model names", () => {
    expect(
      canRetryFailed(
        rollout,
        [{ ...channel, modelGroups: [{ ...group, manufacturer: " proto ", model: " rig " }] }],
        [rollout],
      ),
    ).toBe(true);
  });

  it.each([
    { assignmentGeneration: rollout.assignmentGeneration + 1n },
    { firmwareChecksum: "another-firmware-payload" },
    { firmwareChecksum: "" },
    { activeRolloutId: 999n },
    { manufacturer: "Other" },
    { model: "Other" },
  ])("hides finished retry when the assignment or active-pair precondition is not met (%#)", (patch) => {
    expect(canRetryFailed(rollout, [{ ...channel, modelGroups: [{ ...group, ...patch }] }], [rollout])).toBe(false);
  });

  it("hides finished retry while its channel or model group is missing", () => {
    expect(canRetryFailed(rollout, [], [rollout])).toBe(false);
    expect(canRetryFailed(rollout, [{ ...channel, id: 99n }], [rollout])).toBe(false);
    expect(canRetryFailed(rollout, [{ ...channel, modelGroups: [] }], [rollout])).toBe(false);
  });

  it("also honors an active rollout summary when the channel snapshot has not caught up", () => {
    const active = { ...activeRigRollout, manufacturer: " proto ", model: " rig " };
    expect(canRetryFailed(rollout, [channel], [rollout, active])).toBe(false);
    expect(canRetryFailed(rollout, [channel], [rollout, { ...active, channelId: 99n }])).toBe(true);
    expect(canRetryFailed(rollout, [channel], [rollout, { ...active, manufacturer: "Other" }])).toBe(true);
    expect(canRetryFailed(rollout, [channel], [rollout, { ...active, model: "Other" }])).toBe(true);
    expect(canRetryFailed(rollout, [channel], [rollout, { ...active, status: RolloutStatus.COMPLETED }])).toBe(true);
  });

  it("does not offer retries without failed miners or for canceled and unknown statuses", () => {
    expect(canRetryFailed(completedRigRollout, [channel], [])).toBe(false);
    expect(canRetryFailed({ ...rollout, status: RolloutStatus.CANCELED }, [channel], [])).toBe(false);
    expect(canRetryFailed({ ...rollout, status: RolloutStatus.UNSPECIFIED }, [channel], [])).toBe(false);
  });
});

describe("current assignment completion status", () => {
  it("indexes by finish time regardless of creation/input order and isolates channel and assignment history", () => {
    const olderFinish = create(RolloutSchema, {
      ...completedRigRollout,
      id: 10n,
      createdAt: timestampFromMs(2000),
      finishedAt: timestampFromMs(3000),
    });
    const laterFinish = create(RolloutSchema, {
      ...olderFinish,
      id: 11n,
      manufacturer: " proto ",
      model: " rig ",
      createdAt: timestampFromMs(1000),
      finishedAt: timestampFromMs(4000),
      status: RolloutStatus.COMPLETED_WITH_FAILURES,
    });
    const otherChannel = { ...olderFinish, id: 12n, channelId: 99n };
    const otherAssignment = { ...olderFinish, id: 13n, assignmentGeneration: 99n };
    const canceled = {
      ...laterFinish,
      id: 14n,
      status: RolloutStatus.CANCELED,
      finishedAt: timestampFromMs(5000),
    };
    const rows = [olderFinish, laterFinish, otherChannel, otherAssignment, canceled, activeRigRollout];
    for (const input of [rows, [...rows].reverse()]) {
      const latest = lastFinishedByChannelAssignment(input);
      expect(latest.size).toBe(3);
      expect(latest.get(channelAssignmentKey(olderFinish.channelId, olderFinish))).toBe(laterFinish);
      expect(latest.get(channelAssignmentKey(otherChannel.channelId, otherChannel))).toBe(otherChannel);
      expect(latest.get(channelAssignmentKey(otherAssignment.channelId, otherAssignment))).toBe(otherAssignment);
    }
  });

  it("does not carry an old assignment's failures into the current assignment", () => {
    expect(
      modelUpdateStatus({ ...rigGroup, assignmentGeneration: 2n }, undefined, completedWithFailuresRigRollout),
    ).toEqual({ label: "2 of 6 on target", tone: "none" });
  });

  it("does not credit the current assignment with an old assignment's completion date", () => {
    expect(
      modelUpdateStatus({ ...rigGroup, assignmentGeneration: 2n, onTargetCount: 6 }, undefined, completedRigRollout),
    ).toEqual({ label: "Up to date", tone: "completed" });
  });
});

describe("rolloutStageLabel", () => {
  it("uses the design's stage vocabulary", () => {
    expect(rolloutStageLabel(activeRigRollout)).toBe("In progress");
    expect(rolloutStageLabel(gatedRigRollout)).toBe("Pilot batch review");
    expect(rolloutStageLabel(batchedRigRollout)).toBe("Batch review");
    expect(rolloutStageLabel(pausedRigRollout)).toBe("Paused");
    expect(rolloutStageLabel(completedRigRollout)).toBe("Completed");
    expect(rolloutStageLabel(completedWithFailuresRigRollout)).toBe("Completed with failures");
    expect(rolloutStageLabel(canceledRemainingRigRollout)).toBe("Canceled");
  });

  it("names the batch under way", () => {
    const inBatch = create(RolloutSchema, {
      ...batchedRigRollout,
      state: 1, // IN_PROGRESS
      stage: RolloutStage.BATCH,
      currentBatch: 1,
    });
    expect(rolloutStageLabel(inBatch)).toBe("Batch 2 of 3");
    const pilotBatch = create(RolloutSchema, { ...gatedRigRollout, state: 1, stage: RolloutStage.BATCH });
    expect(rolloutStageLabel(pilotBatch)).toBe("Pilot batch");
  });
});

describe("outcome and pacing labels", () => {
  it("distinguishes cancel reasons", () => {
    expect(rolloutOutcomeLabel(canceledRemainingRigRollout)).toBe("Canceled");
    expect(rolloutOutcomeLabel(completedWithFailuresRigRollout)).toBe("Completed with failures");
  });

  it("summarizes pacing from the behavior", () => {
    expect(pacingSummary(singleBatchBehavior)).toBe("Single batch");
    expect(pacingSummary(pilotBehavior)).toBe("Pilot batch of 2, then remaining");
    expect(pacingSummary(batchedAutoBehavior)).toBe("Batches of 2, auto-continue when healthy");
    expect(pacingSummary({ ...batchedAutoBehavior, autoContinueOnHealthyTelemetry: false })).toBe(
      "Batches of 2, review after each batch",
    );
    expect(pacingSummary({ ...batchedAutoBehavior, reviewAfterEachBatch: false, waitBetweenBatchesSeconds: 900 })).toBe(
      "Batches of 2, 15m between batches",
    );
  });

  it("describes delegated pacing as external control rather than automatic batches", () => {
    const behavior = create(RolloutBehaviorSchema, {
      method: RolloutMethod.DELEGATED,
      controllerTimeoutSeconds: 300,
    });
    expect(pacingSummary(behavior)).toBe("Controlled externally");
  });

  it("reads out the plan for the miners in scope", () => {
    expect(planReadout(batchedAutoBehavior, 48)).toBe("~24 batches of 2 across 48 miners");
    expect(planReadout(pilotBehavior, 48)).toBe("Pilot batch of 2, then 46 remaining");
    expect(planReadout(singleBatchBehavior, 48)).toBe("48 miners in a single batch");
    expect(planReadout(singleBatchBehavior, 0)).toBeNull();
    expect(gatesAfterBatch(pilotBehavior)).toBe(true);
    expect(gatesAfterBatch({ ...batchedAutoBehavior, reviewAfterEachBatch: false })).toBe(false);
  });
});

describe("device counts and progress", () => {
  it("buckets phases into Updated / Remaining / Failed", () => {
    const counts = rolloutDeviceCounts(batchedRigRollout);
    expect(counts).toMatchObject({ updated: 3, failed: 1, queued: 2, total: 6, percent: 50 });
    expect(rolloutProgressSegments(counts)).toEqual([
      { name: "Updated", status: "OK", count: 3 },
      { name: "Remaining", status: "WARNING", count: 2 },
      { name: "Failed", status: "CRITICAL", count: 1 },
    ]);
    expect(rolloutProgressSummary(counts)).toBe("3 of 6 miners updated (50%), 1 failed");
  });

  it("scopes counts to the batch under review", () => {
    expect(scopeCounts(gatedRigRollout)).toMatchObject({ updated: 2, total: 2, percent: 100 });
    expect(scopeCounts(activeRigRollout)).toMatchObject({ updated: 2, total: 6 });
  });

  it.each([
    { remaining: { queued: 1 }, phase: RolloutDevicePhase.QUEUED, suffix: "" },
    { remaining: { failed: 1 }, phase: RolloutDevicePhase.FAILED, suffix: ", 1 failed" },
  ])(
    "keeps the denominator and incomplete percentage with one miner in phase $phase",
    ({ remaining, phase, suffix }) => {
      const summaryCounts = rolloutDeviceCounts(create(RolloutSchema, { deviceCounts: { done: 199, ...remaining } }));
      const fetchedCounts = deviceCounts([
        ...Array.from({ length: 199 }, () => create(RolloutDeviceSchema, { phase: RolloutDevicePhase.DONE })),
        create(RolloutDeviceSchema, { phase }),
      ]);
      expect(fetchedCounts).toEqual(summaryCounts);
      expect(summaryCounts).toMatchObject({ updated: 199, total: 200, percent: 99 });
      expect(rolloutProgressSummary(summaryCounts)).toBe(`199 of 200 miners updated (99%)${suffix}`);
    },
  );

  it.each([
    { done: 1, expected: "1 miner updated (100%)" },
    { done: 200, expected: "200 miners updated (100%)" },
  ])("reports exact completion for $done updated miners, excluding neutral targets", ({ done, expected }) => {
    const counts = rolloutDeviceCounts(create(RolloutSchema, { deviceCounts: { done, excluded: 2, skipped: 1 } }));
    expect(counts).toMatchObject({ updated: done, total: done, percent: 100 });
    expect(rolloutProgressSummary(counts)).toBe(expected);
  });

  it("does not report completion for an empty or entirely neutral scope", () => {
    for (const deviceCounts of [{}, { excluded: 2, skipped: 1 }]) {
      const counts = rolloutDeviceCounts(create(RolloutSchema, { deviceCounts }));
      expect(counts).toMatchObject({ updated: 0, total: 0, percent: 0 });
      expect(rolloutProgressSummary(counts)).toBe("0 of 0 miners updated (0%)");
    }
  });

  it("tallies a fetched device page the same way the server does", () => {
    const devices = rolloutDevices[batchedRigRollout.id.toString()] ?? [];
    expect(deviceCounts(devices)).toEqual(rolloutDeviceCounts(batchedRigRollout));
    expect(scopeDevices(batchedRigRollout, devices).map((d) => d.deviceIdentifier)).toEqual(["rig-003", "rig-004"]);
    expect(failedDevices(devices).map((d) => d.deviceIdentifier)).toEqual(["rig-004"]);
    expect(scopeDevices(activeRigRollout, devices)).toHaveLength(6);
  });

  it.each([RolloutMethod.BATCHED, RolloutMethod.PILOT_THEN_CONTINUE])(
    "limits REST evidence to unbatched targets while retaining whole-rollout progress for method %s",
    (method) => {
      const devices = [
        create(RolloutDeviceSchema, { deviceIdentifier: "completed-batch", batch: 1, phase: RolloutDevicePhase.DONE }),
        create(RolloutDeviceSchema, {
          deviceIdentifier: "drifted-batch",
          batch: 1,
          phase: RolloutDevicePhase.IN_PROGRESS,
        }),
        create(RolloutDeviceSchema, { deviceIdentifier: "remaining-done", phase: RolloutDevicePhase.DONE }),
        create(RolloutDeviceSchema, { deviceIdentifier: "late-joiner", phase: RolloutDevicePhase.QUEUED }),
        create(RolloutDeviceSchema, { deviceIdentifier: "remaining-failed", phase: RolloutDevicePhase.FAILED }),
        create(RolloutDeviceSchema, { deviceIdentifier: "remaining-excluded", phase: RolloutDevicePhase.EXCLUDED }),
        create(RolloutDeviceSchema, { deviceIdentifier: "remaining-skipped", phase: RolloutDevicePhase.SKIPPED }),
      ];
      const rollout = create(RolloutSchema, {
        ...activeRigRollout,
        behavior: create(RolloutBehaviorSchema, { method }),
        batchCount: 1,
        stage: RolloutStage.REST,
        deviceCount: devices.length,
        deviceCounts: create(RolloutDeviceCountsSchema, {
          done: 2,
          inProgress: 1,
          queued: 1,
          failed: 1,
          excluded: 1,
          skipped: 1,
        }),
        currentBatchCounts: create(RolloutDeviceCountsSchema),
        evidence: create(RolloutEvidenceSchema, { devicesTotal: 5, verified: 1, failed: 1, excluded: 1, skipped: 1 }),
      });

      const evidenceDevices = scopeDevices(rollout, devices);
      expect(evidenceDevices.map((device) => device.deviceIdentifier)).toEqual([
        "remaining-done",
        "late-joiner",
        "remaining-failed",
        "remaining-excluded",
        "remaining-skipped",
      ]);
      expect(evidenceDevices).toHaveLength(rollout.evidence!.devicesTotal);
      expect(deviceCounts(evidenceDevices)).toMatchObject({ updated: 1, failed: 1, excluded: 1, skipped: 1, total: 3 });
      expect(scopedToBatch(rollout)).toBe(false);
      expect(evidenceScopeLabel(rollout)).toBe("Remaining miners");
      expect(scopeCounts(rollout)).toEqual(deviceCounts(devices));
      expect(scopeCounts(rollout)).toMatchObject({ updated: 2, updating: 1, failed: 1, total: 5 });

      const completedDevices = devices.map((device) =>
        create(RolloutDeviceSchema, {
          ...device,
          phase:
            device.phase === RolloutDevicePhase.IN_PROGRESS || device.phase === RolloutDevicePhase.QUEUED
              ? RolloutDevicePhase.DONE
              : device.phase,
        }),
      );
      const completed = create(RolloutSchema, {
        ...rollout,
        status: RolloutStatus.COMPLETED_WITH_FAILURES,
        state: RolloutState.COMPLETED_WITH_FAILURES,
        deviceCounts: create(RolloutDeviceCountsSchema, { done: 4, failed: 1, excluded: 1, skipped: 1 }),
        evidence: undefined,
      });
      expect(scopeDevices(completed, completedDevices)).toBe(completedDevices);
      expect(evidenceScopeLabel(completed)).toBe("All miners");
      expect(scopeCounts(completed)).toEqual(deviceCounts(completedDevices));
    },
  );

  it.each([RolloutStage.BATCH, RolloutStage.AWAITING_REVIEW, RolloutStage.WAITING])(
    "keeps evidence and progress on the current positive batch in stage %s",
    (stage) => {
      const rollout = create(RolloutSchema, { ...batchedRigRollout, stage });
      const devices = rolloutDevices[rollout.id.toString()] ?? [];
      expect(scopedToBatch(rollout)).toBe(true);
      expect(evidenceScopeLabel(rollout)).toBe("Batch 2 of 3");
      expect(scopeDevices(rollout, devices).map((device) => device.deviceIdentifier)).toEqual(["rig-003", "rig-004"]);
      expect(scopeCounts(rollout)).toEqual(deviceCounts(scopeDevices(rollout, devices)));
    },
  );

  it.each([RolloutMethod.ALL_AT_ONCE, RolloutMethod.DELEGATED])(
    "keeps every target in evidence and progress for an unbatched method %s",
    (method) => {
      const rollout = create(RolloutSchema, {
        ...activeRigRollout,
        behavior: create(RolloutBehaviorSchema, { method }),
      });
      const devices = rolloutDevices[activeRigRollout.id.toString()] ?? [];
      expect(scopedToBatch(rollout)).toBe(false);
      expect(scopeDevices(rollout, devices)).toBe(devices);
      expect(evidenceScopeLabel(rollout)).toBe("All miners");
      expect(scopeCounts(rollout)).toEqual(deviceCounts(devices));
    },
  );

  it("summarizes an active update for banners and the header pill", () => {
    expect(activeUpdateSummary(activeRigRollout)).toBe("2 of 6 miners updated");
    expect(activeUpdateSummary(gatedRigRollout)).toBe("2 of 6 miners updated, Pilot batch review");
    expect(activeUpdateSummary(batchedRigRollout)).toBe("3 of 6 miners updated, 1 failed, Batch review");
    expect(activeUpdateSummary(pausedRigRollout)).toBe("2 of 6 miners updated, Paused");
  });

  it("flags rollouts that need a human", () => {
    expect(rolloutNeedsAttention(gatedRigRollout)).toBe(true);
    expect(rolloutNeedsAttention(batchedRigRollout)).toBe(true);
    expect(rolloutNeedsAttention(activeRigRollout)).toBe(false);
    expect(rolloutNeedsAttention(completedWithFailuresRigRollout)).toBe(false);
  });
});

describe("channel and model status", () => {
  it("distinguishes an unavailable assignment from an unassigned or available model", () => {
    expect(hasUnavailableAssignedFirmware({ ...rigGroup, firmwareAvailable: false, firmwareFileId: "" })).toBe(true);
    expect(hasUnavailableAssignedFirmware(rigGroup)).toBe(false);
    expect(hasUnavailableAssignedFirmware(canaryChannel.modelGroups[1])).toBe(false);
  });

  it.each([
    { name: "active", rollout: activeRigRollout },
    { name: "paused", rollout: pausedRigRollout },
    { name: "reviewing", rollout: gatedRigRollout },
    { name: "settled", rollout: undefined },
  ])("surfaces unavailable assigned firmware for a $name model", ({ rollout }) => {
    const group = {
      ...rigGroup,
      firmwareFileId: "",
      firmwareAvailable: false,
      onTargetCount: rigGroup.minerCount,
    };
    expect(modelUpdateStatus(group, rollout, completedRigRollout)).toEqual({
      label: "Assigned firmware unavailable",
      tone: "attention",
    });
  });

  it("surfaces an unavailable assignment even before its current rollout summary arrives", () => {
    expect(
      modelUpdateStatus(
        { ...rigGroup, activeRolloutId: 99n, firmwareAvailable: false, firmwareFileId: "" },
        undefined,
        undefined,
      ),
    ).toEqual({ label: "Assigned firmware unavailable", tone: "attention" });
  });

  it("describes the model's active update", () => {
    expect(modelUpdateStatus(rigGroup, activeRigRollout, undefined)).toEqual({
      label: "Updating, 2 of 6",
      tone: "active",
    });
    expect(modelUpdateStatus(rigGroup, gatedRigRollout, undefined)).toEqual({
      label: "Review needed",
      tone: "attention",
    });
    expect(modelUpdateStatus(rigGroup, batchedRigRollout, undefined)).toEqual({
      label: "1 failed, 1 of 2 updated",
      tone: "attention",
    });
    expect(modelUpdateStatus(rigGroup, pausedRigRollout, undefined)).toEqual({ label: "Paused, 2 of 6", tone: "none" });
  });

  it("describes a settled model against its assignment", () => {
    const settled = { ...rigGroup, onTargetCount: rigGroup.minerCount, reportedVersions: ["1.4.4"] };
    expect(modelUpdateStatus(settled, undefined, completedRigRollout).label).toMatch(/^Updated /);
    expect(modelUpdateStatus(rigGroup, undefined, undefined)).toEqual({ label: "2 of 6 on target", tone: "none" });
    expect(modelUpdateStatus(rigGroup, undefined, completedWithFailuresRigRollout)).toEqual({
      label: "1 failed to update",
      tone: "attention",
    });
    expect(modelUpdateStatus(canaryChannel.modelGroups[1], undefined, undefined)).toEqual({
      label: "No firmware assigned",
      tone: "none",
    });
  });

  it("distinguishes an inter-batch wait from updating while preserving pause precedence", () => {
    const waiting = create(RolloutSchema, {
      ...batchedRigRollout,
      state: RolloutState.IN_PROGRESS,
      stage: RolloutStage.WAITING,
      behavior: create(RolloutBehaviorSchema, {
        method: RolloutMethod.BATCHED,
        batchSize: 2,
        waitBetweenBatchesSeconds: 60,
      }),
      currentBatchCounts: create(RolloutDeviceCountsSchema, { done: 2 }),
      deviceCounts: create(RolloutDeviceCountsSchema, { done: 4, queued: 2 }),
    });
    expect(modelUpdateStatus(rigGroup, waiting, undefined)).toEqual({
      label: "Waiting for the next batch",
      tone: "active",
    });
    expect(rolloutStageLabel(waiting)).toBe("Waiting for the next batch");
    expect(modelUpdateStatus(rigGroup, { ...waiting, state: RolloutState.PAUSED }, undefined)).toEqual({
      label: "Paused, 2 of 2",
      tone: "none",
    });
    expect(
      modelUpdateStatus(
        rigGroup,
        { ...waiting, currentBatchCounts: create(RolloutDeviceCountsSchema, { done: 1, failed: 1 }) },
        undefined,
      ),
    ).toEqual({ label: "1 failed, 1 of 2 updated", tone: "attention" });
  });

  it("does not count skipped, excluded or newly joined off-target miners as failures", () => {
    const finished = create(RolloutSchema, {
      ...completedWithFailuresRigRollout,
      deviceCounts: create(RolloutDeviceCountsSchema, { done: 2, failed: 1, skipped: 2, excluded: 1 }),
    });
    const grownGroup = { ...rigGroup, minerCount: 10, onTargetCount: 2 };
    expect(modelUpdateStatus(grownGroup, undefined, finished)).toEqual({
      label: "1 failed to update",
      tone: "attention",
    });
    expect(modelUpdateStatus({ ...grownGroup, onTargetCount: 10 }, undefined, finished).label).toMatch(/^Updated /);
  });

  it.each([
    {
      name: "waiting for an external controller",
      state: RolloutState.WAITING_FOR_CONTROLLER,
      counts: { done: 2, queued: 4 },
      expected: { label: "Waiting for controller", tone: "active" },
    },
    {
      name: "waiting with failed miners",
      state: RolloutState.WAITING_FOR_CONTROLLER,
      counts: { done: 2, failed: 1, queued: 3 },
      expected: { label: "1 failed, 2 of 6 updated", tone: "attention" },
    },
    {
      name: "paused with failed miners",
      state: RolloutState.PAUSED,
      counts: { done: 2, failed: 1, queued: 3 },
      expected: { label: "Paused, 2 of 6", tone: "none" },
    },
    {
      name: "updating after controller dispatch",
      state: RolloutState.IN_PROGRESS,
      counts: { done: 2, inProgress: 1, queued: 3 },
      expected: { label: "Updating, 2 of 6", tone: "active" },
    },
  ])("describes delegated model status when $name", ({ state, counts, expected }) => {
    const delegated = create(RolloutSchema, {
      ...activeRigRollout,
      behavior: create(RolloutBehaviorSchema, { method: RolloutMethod.DELEGATED }),
      stage: RolloutStage.REST,
      state,
      deviceCounts: create(RolloutDeviceCountsSchema, counts),
    });
    expect(modelUpdateStatus(rigGroup, delegated, undefined)).toEqual(expected);
  });

  it("shows remaining off-target miners neutrally when the completed rollout only skipped them", () => {
    const finished = create(RolloutSchema, {
      ...completedRigRollout,
      deviceCounts: create(RolloutDeviceCountsSchema, { done: 2, skipped: 4 }),
    });
    expect(modelUpdateStatus(rigGroup, undefined, finished)).toEqual({
      label: "2 of 6 on target",
      tone: "none",
    });
  });

  it("rolls channel updates up", () => {
    expect(channelUpdateStatus([])).toEqual({ label: "No active updates", tone: "none" });
    expect(channelUpdateStatus([activeRigRollout, gatedRigRollout, pausedRigRollout])).toEqual({
      label: "3 updating, 1 needs attention, 1 paused",
      tone: "attention",
    });
  });

  it("shows firmware transitions", () => {
    expect(modelFirmwareLabel(rigGroup)).toBe("1.4.3 → 1.4.4");
    expect(modelFirmwareLabel(canaryChannel.modelGroups[1])).toBe("—");
  });
});

describe("scope summary", () => {
  it("labels populated dimensions", () => {
    expect(scopeSummary(canaryChannel.scope!)).toBe("1 rack, 2 miners");
    expect(scopeSummary(create(ReleaseChannelScopeSchema))).toBe("No miners selected");
    expect(isScopeEmpty(create(ReleaseChannelScopeSchema))).toBe(true);
    expect(isScopeEmpty(canaryChannel.scope!)).toBe(false);
  });
});

describe("metricDisplay", () => {
  it("colors deltas by outcome, not sign", () => {
    expect(metricDisplay("hashrate", { baseline: 100e12, current: 110e12 }, "C")).toMatchObject({
      delta: "+10.0%",
      deltaIntent: "positive",
    });
    expect(metricDisplay("efficiency", { baseline: 30, current: 33 }, "C")).toMatchObject({
      delta: "+10.0%",
      deltaIntent: "negative",
    });
    expect(metricDisplay("temperature", { baseline: 64, current: 66 }, "C")).toMatchObject({
      delta: "+2.0 °C",
      deltaIntent: "negative",
    });
    expect(metricDisplay("power", { baseline: 3000, current: undefined }, "C")).toMatchObject({ value: "—" });
  });
});
