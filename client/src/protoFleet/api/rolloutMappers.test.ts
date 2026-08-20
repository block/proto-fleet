import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";

import {
  FirmwareTransitionMinerSchema,
  FirmwareTransitionState,
  PreviewRolloutLaneMembershipChangeResponseSchema,
  RolloutBatchEvidenceSummarySchema,
  RolloutBatchSchema,
  RolloutBatchState,
  RolloutEvidencePhase,
  RolloutEvidenceSchema,
  RolloutEvidenceStatus,
  RolloutHashratePolicySchema,
  RolloutLaneChannelSchema,
  RolloutLaneFirmwareConvergenceStatusSchema,
  RolloutLaneMemberSchema,
  RolloutLaneMembershipReassignmentSchema,
  RolloutLanePreviewSchema,
  RolloutLaneSchema,
  RolloutMemberSchema,
  RolloutMemberState,
  RolloutSchema,
  RolloutState,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import {
  getRolloutActionEligibility,
  mapRollout,
  mapRolloutEvidenceStatus,
  mapRolloutLane,
  mapRolloutLaneMembershipChangePreview,
  mapRolloutLanePreview,
  mapRolloutMemberState,
  mapRolloutState,
  mapRolloutToEvent,
  rolloutMemberStateToTargetPhase,
} from "@/protoFleet/api/rolloutMappers";

const timestamp = (iso: string) => timestampFromDate(new Date(iso));

describe("rollout mappers", () => {
  it("retains reassignment confirmation tokens from lane previews", () => {
    const preview = create(RolloutLanePreviewSchema, {
      requiresReassignmentConfirmation: true,
      reassignmentConfirmationToken: "preview-token",
    });

    expect(mapRolloutLanePreview(preview).reassignmentConfirmationToken).toBe("preview-token");
  });

  it("maps stable lane identity without exposing physical channel labels", () => {
    const lane = create(RolloutLaneSchema, {
      laneId: "15bc6181-07d8-45ac-8424-50b5e938b871",
      label: "Stable production",
      description: "Production firmware lane",
      currentChannelId: 41n,
      revision: 3n,
      memberCount: 12,
      channels: [
        create(RolloutLaneChannelSchema, {
          channelId: 41n,
          releaseSetId: 7n,
          position: 0,
          current: true,
        }),
      ],
      firmwareConvergence: create(RolloutLaneFirmwareConvergenceStatusSchema, {
        totalCount: 12,
        pendingCount: 2,
        updatingCount: 1,
        verifyingCount: 1,
        confirmedCount: 8,
        attentionCount: 0,
        members: [
          create(FirmwareTransitionMinerSchema, {
            deviceIdentifier: "miner-1",
            manufacturer: "Proto",
            model: "Alpha",
            latestObservedFirmwareVersion: "1.0.0",
            targetFirmwareVersion: "2.0.0",
            state: FirmwareTransitionState.VERIFYING,
            updatedAt: timestamp("2026-08-18T00:59:00Z"),
          }),
          create(FirmwareTransitionMinerSchema, {
            deviceIdentifier: "miner-2",
            manufacturer: "Proto",
            model: "Alpha",
            targetFirmwareVersion: "2.0.0",
            state: FirmwareTransitionState.NEEDS_ATTENTION,
            lastError: "Firmware identity could not be confirmed",
          }),
        ],
      }),
      updatedAt: timestamp("2026-08-18T01:00:00Z"),
    });

    expect(
      mapRolloutLane(lane, {
        memberIdentifiers: ["miner-1", "miner-2"],
        releaseTargets: [
          {
            firmwareFileId: "file-alpha",
            targetManufacturer: "Proto",
            targetModel: "Alpha",
            firmwareVersion: "1.0.0",
            sha256: "abc",
          },
        ],
      }),
    ).toMatchObject({
      id: "15bc6181-07d8-45ac-8424-50b5e938b871",
      label: "Stable production",
      currentChannelId: 41n,
      memberCount: 12,
      memberIdentifiers: ["miner-1", "miner-2"],
      currentReleaseTargets: [{ targetModel: "Alpha", firmwareVersion: "1.0.0" }],
      firmwareConvergence: {
        totalCount: 12,
        pendingCount: 2,
        updatingCount: 1,
        verifyingCount: 1,
        confirmedCount: 8,
        attentionCount: 0,
        members: [
          {
            deviceIdentifier: "miner-1",
            latestObservedFirmwareVersion: "1.0.0",
            state: "verifying",
            updatedAt: timestamp("2026-08-18T00:59:00Z"),
          },
          {
            deviceIdentifier: "miner-2",
            latestObservedFirmwareVersion: undefined,
            state: "needsAttention",
            lastError: "Firmware identity could not be confirmed",
          },
        ],
      },
      updatedAt: "2026-08-18T01:00:00.000Z",
    });
  });

  it("maps rollout lane membership previews and transition details", () => {
    const preview = create(PreviewRolloutLaneMembershipChangeResponseSchema, {
      targetFirmwarePreview: create(RolloutLanePreviewSchema, {
        matchingCount: 1,
      }),
      reassignments: [
        create(RolloutLaneMembershipReassignmentSchema, {
          deviceIdentifier: "miner-reassign",
          sourceLaneId: "223da5d0-ab28-4f8e-95e5-496e4804317c",
          sourceLaneLabel: "Canary",
        }),
      ],
      removals: [
        create(RolloutLaneMemberSchema, {
          deviceIdentifier: "miner-remove",
          manufacturer: "Proto",
          model: "Alpha",
          observedFirmwareVersion: "1.0.0",
          channelId: 41n,
          channelPosition: 0,
          onCurrentChannel: true,
          pinnedReleaseVersion: "1.0.0",
        }),
      ],
      requiresFirmwareConfirmation: true,
      requiresReassignmentConfirmation: true,
    });

    expect(mapRolloutLaneMembershipChangePreview(preview)).toMatchObject({
      targetFirmwarePreview: { matchingCount: 1 },
      reassignments: [
        {
          deviceIdentifier: "miner-reassign",
          sourceLaneLabel: "Canary",
        },
      ],
      removals: [
        {
          deviceIdentifier: "miner-remove",
          channelId: 41n,
          pinnedReleaseVersion: "1.0.0",
        },
      ],
      requiresFirmwareConfirmation: true,
      requiresReassignmentConfirmation: true,
    });
  });

  it("rejects membership previews that omit the target firmware preview", () => {
    const preview = create(PreviewRolloutLaneMembershipChangeResponseSchema);

    expect(() => mapRolloutLaneMembershipChangePreview(preview)).toThrow(
      "Rollout lane membership preview response is missing its target firmware preview.",
    );
  });

  it.each([
    [RolloutState.CREATED, "created"],
    [RolloutState.RUNNING, "running"],
    [RolloutState.PAUSED, "paused"],
    [RolloutState.REVIEW, "review"],
    [RolloutState.ABORTED, "aborted"],
    [RolloutState.COMPLETED, "completed"],
    [RolloutState.COMPLETED_WITH_FAILURES, "completedWithFailures"],
    [RolloutState.REVERTING, "reverting"],
    [RolloutState.REVERTED, "reverted"],
  ] as const)("maps rollout state %s", (state, expected) => {
    expect(mapRolloutState(state)).toBe(expected);
  });

  it.each([
    [RolloutMemberState.PENDING, "pending"],
    [RolloutMemberState.ADMITTED, "admitted"],
    [RolloutMemberState.SUCCEEDED, "succeeded"],
    [RolloutMemberState.FAILED, "failed"],
    [RolloutMemberState.ATTENTION_REQUIRED, "attentionRequired"],
    [RolloutMemberState.CANCELLED, "cancelled"],
    [RolloutMemberState.REVERTING, "reverting"],
    [RolloutMemberState.REVERTED, "reverted"],
  ] as const)("maps member state %s", (state, expected) => {
    expect(mapRolloutMemberState(state)).toBe(expected);
  });

  it.each([
    [RolloutEvidenceStatus.PENDING, "pending"],
    [RolloutEvidenceStatus.COLLECTING, "collecting"],
    [RolloutEvidenceStatus.UNAVAILABLE, "unavailable"],
    [RolloutEvidenceStatus.OBSERVING, "observing"],
    [RolloutEvidenceStatus.HEALTHY, "healthy"],
    [RolloutEvidenceStatus.HELD, "held"],
    [RolloutEvidenceStatus.STALE, "stale"],
    [RolloutEvidenceStatus.AUTOMATION_ERROR, "automationError"],
    [RolloutEvidenceStatus.FINALIZED, "finalized"],
  ] as const)("maps evidence status %s", (status, expected) => {
    expect(mapRolloutEvidenceStatus(status)).toBe(expected);
  });

  it("maps optional timestamps and preserves unavailable evidence", () => {
    const rollout = create(RolloutSchema, {
      rolloutId: "2f214a71-f94e-4e5f-8daf-d36c71b72f6c",
      name: "Firmware rollout",
      strategyKey: "fixture-strategy",
      state: RolloutState.RUNNING,
      revision: 4n,
      startedAt: timestamp("2026-08-18T01:00:00Z"),
      members: [
        create(RolloutMemberSchema, {
          memberId: 11n,
          batchId: 7n,
          deviceIdentifier: "miner-1",
          state: RolloutMemberState.ATTENTION_REQUIRED,
          lastError: "Firmware result is ambiguous",
          evidence: [
            create(RolloutEvidenceSchema, {
              evidenceId: 21n,
              phase: RolloutEvidencePhase.POST,
              windowStart: timestamp("2026-08-18T01:05:00Z"),
              windowEnd: timestamp("2026-08-18T01:10:00Z"),
              errorCount: 0n,
            }),
          ],
        }),
      ],
    });

    const mapped = mapRollout(rollout);

    expect(mapped.startedAt).toBe("2026-08-18T01:00:00.000Z");
    expect(mapped.pausedAt).toBeUndefined();
    expect(mapped.members[0]).toMatchObject({
      id: 11n,
      state: "attentionRequired",
      lastError: "Firmware result is ambiguous",
    });
    expect(mapped.members[0].evidence[0]).toMatchObject({
      phase: "post",
      observedAt: undefined,
      avgHashrateHs: undefined,
      avgPowerW: undefined,
      avgTemperatureC: undefined,
      errorCount: 0n,
      sampleCount: undefined,
    });
  });

  it("maps policy and selects only the latest completed batch summary for performance", () => {
    const rollout = create(RolloutSchema, {
      rolloutId: "2f214a71-f94e-4e5f-8daf-d36c71b72f6c",
      name: "Production 2.0.0",
      strategyKey: "between-channel",
      state: RolloutState.REVIEW,
      hashratePolicy: create(RolloutHashratePolicySchema, {
        maxDropBasisPoints: 10,
        healthyDurationSeconds: 30,
      }),
      batches: [
        create(RolloutBatchSchema, {
          batchId: 7n,
          position: 0,
          label: "Pilot",
          state: RolloutBatchState.COMPLETED,
          completedAt: timestamp("2026-08-18T01:00:00Z"),
          evidenceSummary: create(RolloutBatchEvidenceSummarySchema, {
            status: RolloutEvidenceStatus.FINALIZED,
            totalCount: 1n,
            pairedCount: 1n,
            cumulativeBaselineHashrateHs: 80_000_000_000_000,
            cumulativeCurrentHashrateHs: 88_000_000_000_000,
            cumulativeDeltaBasisPoints: 1_000,
            evaluatedAt: timestamp("2026-08-18T01:30:00Z"),
            postWindowFinalized: true,
            postWindowFinalizedAt: timestamp("2026-08-18T01:30:00Z"),
          }),
        }),
        create(RolloutBatchSchema, {
          batchId: 8n,
          position: 1,
          label: "Remaining",
          state: RolloutBatchState.COMPLETED,
          completedAt: timestamp("2026-08-18T02:00:00Z"),
          evidenceSummary: create(RolloutBatchEvidenceSummarySchema, {
            status: RolloutEvidenceStatus.HELD,
            totalCount: 3n,
            pairedCount: 2n,
            cumulativeBaselineHashrateHs: 250_000_000_000_000,
            cumulativeCurrentHashrateHs: 237_500_000_000_000,
            cumulativeDeltaBasisPoints: -500,
            latestPolicyBucketHashrateHs: 240_000_000_000_000,
            latestPolicyBucketDeltaBasisPoints: -400,
            healthySince: timestamp("2026-08-18T02:00:20Z"),
            lastPolicyBucketBoundary: timestamp("2026-08-18T02:00:30Z"),
            evaluatedAt: timestamp("2026-08-18T02:00:35Z"),
            postWindowFinalized: false,
            errorMessage: "Automatic continue failed after the control started",
          }),
        }),
        create(RolloutBatchSchema, {
          batchId: 9n,
          position: 2,
          label: "Queued",
          state: RolloutBatchState.PENDING,
        }),
      ],
      members: [
        create(RolloutMemberSchema, {
          memberId: 11n,
          batchId: 7n,
          deviceIdentifier: "old-batch-member",
          evidence: [
            create(RolloutEvidenceSchema, {
              phase: RolloutEvidencePhase.BASELINE,
              avgHashrateHs: 1,
              avgPowerW: 1_000,
              avgTemperatureC: 60,
            }),
            create(RolloutEvidenceSchema, {
              phase: RolloutEvidencePhase.POST,
              avgHashrateHs: 2,
              avgPowerW: 2_000,
              avgTemperatureC: 70,
            }),
          ],
        }),
      ],
    });

    const mapped = mapRollout(rollout);
    const event = mapRolloutToEvent(mapped);

    expect(mapped.hashratePolicy).toEqual({
      maxDropBasisPoints: 10,
      healthyDurationSeconds: 30,
    });
    expect(mapped.batches[1]).toMatchObject({
      completedAt: "2026-08-18T02:00:00.000Z",
      evidenceSummary: {
        status: "held",
        totalCount: 3n,
        pairedCount: 2n,
        cumulativeDeltaBasisPoints: -500,
        latestPolicyBucketDeltaBasisPoints: -400,
        healthySince: "2026-08-18T02:00:20.000Z",
        lastPolicyBucketBoundary: "2026-08-18T02:00:30.000Z",
        evaluatedAt: "2026-08-18T02:00:35.000Z",
        postWindowFinalized: false,
        errorMessage: "Automatic continue failed after the control started",
      },
    });
    expect(event.autoContinueOnHealthyTelemetry).toBe(true);
    expect(event.performance).toEqual({
      metrics: [{ label: "Hashrate", unit: "hashrate", baseline: 250, current: 237.5 }],
    });
    expect(event.evidence).toMatchObject({
      batchId: 8n,
      batchLabel: "Remaining",
      status: "held",
      pairedCount: 2n,
      totalCount: 3n,
      cumulativeDeltaBasisPoints: -500,
      latestPolicyBucketDeltaBasisPoints: -400,
      errorMessage: "Automatic continue failed after the control started",
      policy: {
        maxDropBasisPoints: 10,
        healthyDurationSeconds: 30,
      },
    });
  });

  it("does not fall back to an older batch when the latest completed summary is absent", () => {
    const mapped = mapRollout(
      create(RolloutSchema, {
        rolloutId: "2f214a71-f94e-4e5f-8daf-d36c71b72f6c",
        state: RolloutState.RUNNING,
        batches: [
          create(RolloutBatchSchema, {
            batchId: 7n,
            position: 0,
            state: RolloutBatchState.COMPLETED,
            evidenceSummary: create(RolloutBatchEvidenceSummarySchema, {
              status: RolloutEvidenceStatus.FINALIZED,
              totalCount: 1n,
              pairedCount: 1n,
              cumulativeBaselineHashrateHs: 100_000_000_000_000,
              cumulativeCurrentHashrateHs: 110_000_000_000_000,
              postWindowFinalized: true,
            }),
          }),
          create(RolloutBatchSchema, {
            batchId: 8n,
            position: 1,
            state: RolloutBatchState.COMPLETED,
          }),
        ],
      }),
    );

    expect(mapped.batches[1]).toMatchObject({
      state: "completed",
      completedAt: undefined,
      evidenceSummary: undefined,
    });
    expect(mapRolloutToEvent(mapped).performance).toBeUndefined();
    expect(mapRolloutToEvent(mapped).evidence).toBeUndefined();
  });

  it("preserves fixture eligibility alongside server lifecycle states", () => {
    expect(getRolloutActionEligibility("pausedAtPilotGate")).toMatchObject({
      continue: true,
      abort: true,
      pause: false,
    });
    expect(getRolloutActionEligibility("stabilizingTelemetry")).toMatchObject({
      continue: false,
      abort: true,
      pause: false,
    });
  });

  it("maps attention-required members to a non-retry presentation phase", () => {
    expect(rolloutMemberStateToTargetPhase("attentionRequired")).toBe("attentionRequired");
  });

  it("derives membership separately from terminal firmware convergence", () => {
    const rollout = mapRollout(
      create(RolloutSchema, {
        rolloutId: "2f214a71-f94e-4e5f-8daf-d36c71b72f6c",
        name: "Production 2.0.0",
        strategyKey: "between-channel",
        state: RolloutState.RUNNING,
        members: [
          create(RolloutMemberSchema, {
            memberId: 1n,
            deviceIdentifier: "confirmed",
            state: RolloutMemberState.SUCCEEDED,
          }),
          create(RolloutMemberSchema, {
            memberId: 2n,
            deviceIdentifier: "attention",
            state: RolloutMemberState.ATTENTION_REQUIRED,
            lastError: "Firmware result is ambiguous",
          }),
          create(RolloutMemberSchema, {
            memberId: 3n,
            deviceIdentifier: "queued",
            state: RolloutMemberState.PENDING,
          }),
        ],
      }),
    );

    const event = mapRolloutToEvent(rollout, { laneLabel: "Stable production" });

    expect(event.membershipProgress).toEqual({ completed: 1, total: 3 });
    expect(event.convergenceProgress).toEqual({
      completed: 2,
      total: 3,
      attentionRequired: 1,
      failed: 0,
    });
    expect(event.rollups).toEqual(
      expect.arrayContaining([
        { phase: "done", count: 1 },
        { phase: "attentionRequired", count: 1 },
        { phase: "queued", count: 1 },
      ]),
    );
    expect(event.errors).toEqual([
      {
        id: "Firmware result is ambiguous",
        message: "Firmware result is ambiguous",
        impactedMiners: ["attention"],
      },
    ]);
  });
});
