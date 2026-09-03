import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import { gatesAfterBatch, planReadout } from "./behaviorUtils";
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
  singleBatchBehavior,
} from "./ReleaseChannels.fixtures";
import {
  activeUpdateSummary,
  channelUpdateStatus,
  metricDisplay,
  modelFirmwareLabel,
  modelUpdateStatus,
  pacingSummary,
  rolloutDeviceCounts,
  rolloutNeedsAttention,
  rolloutOutcomeLabel,
  rolloutProgressSegments,
  rolloutProgressSummary,
  rolloutStageLabel,
  scopeCounts,
} from "./rolloutStatus";
import { isScopeEmpty, scopeSummary } from "./scopeUtils";
import {
  ReleaseChannelScopeSchema,
  RolloutSchema,
  RolloutStage,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

const rigGroup = canaryChannel.modelGroups[0];

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
    const settled = { ...rigGroup, miners: rigGroup.miners.map((m) => ({ ...m, firmwareVersion: "1.4.4" })) };
    expect(modelUpdateStatus(settled, undefined, completedRigRollout).label).toMatch(/^Updated /);
    expect(modelUpdateStatus(rigGroup, undefined, undefined)).toEqual({ label: "2 of 6 on target", tone: "none" });
    expect(modelUpdateStatus(rigGroup, undefined, completedWithFailuresRigRollout)).toEqual({
      label: "4 failed to update",
      tone: "attention",
    });
    expect(modelUpdateStatus(canaryChannel.modelGroups[1], undefined, undefined)).toEqual({
      label: "No firmware assigned",
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
