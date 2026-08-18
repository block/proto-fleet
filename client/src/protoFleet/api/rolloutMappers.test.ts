import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";

import {
  RolloutEvidencePhase,
  RolloutEvidenceSchema,
  RolloutLaneChannelSchema,
  RolloutLaneSchema,
  RolloutMemberSchema,
  RolloutMemberState,
  RolloutSchema,
  RolloutState,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import {
  mapRollout,
  mapRolloutLane,
  mapRolloutMemberState,
  mapRolloutState,
  mapRolloutToEvent,
  rolloutMemberStateToTargetPhase,
} from "@/protoFleet/api/rolloutMappers";

const timestamp = (iso: string) =>
  create(TimestampSchema, {
    seconds: BigInt(Math.floor(new Date(iso).getTime() / 1000)),
  });

describe("rollout mappers", () => {
  it("maps stable lane identity without exposing physical channel labels", () => {
    const lane = create(RolloutLaneSchema, {
      laneId: "15bc6181-07d8-45ac-8424-50b5e938b871",
      label: "Stable production",
      description: "Production firmware lane",
      currentChannelId: 41n,
      revision: 3n,
      channels: [
        create(RolloutLaneChannelSchema, {
          channelId: 41n,
          releaseSetId: 7n,
          position: 0,
          current: true,
        }),
      ],
      updatedAt: timestamp("2026-08-18T01:00:00Z"),
    });

    expect(
      mapRolloutLane(lane, {
        memberCount: 12,
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
      currentReleaseSetId: 7n,
      memberCount: 12,
      memberIdentifiers: ["miner-1", "miner-2"],
      currentReleaseTargets: [{ targetModel: "Alpha", firmwareVersion: "1.0.0" }],
      updatedAt: "2026-08-18T01:00:00.000Z",
    });
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

  it("keeps membership and convergence progress independent", () => {
    const rollout = create(RolloutSchema, {
      rolloutId: "2f214a71-f94e-4e5f-8daf-d36c71b72f6c",
      name: "Firmware rollout",
      strategyKey: "fixture-strategy",
      state: RolloutState.RUNNING,
    });

    const mapped = mapRollout(rollout, {
      membershipProgress: { completed: 3, total: 10 },
      convergenceProgress: { completed: 7, total: 10, attentionRequired: 1 },
    });

    expect(mapped.membershipProgress).toEqual({ completed: 3, total: 10 });
    expect(mapped.convergenceProgress).toEqual({ completed: 7, total: 10, attentionRequired: 1 });
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
