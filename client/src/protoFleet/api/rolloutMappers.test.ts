import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";

import {
  RolloutEvidencePhase,
  RolloutEvidenceSchema,
  RolloutMemberSchema,
  RolloutMemberState,
  RolloutSchema,
  RolloutState,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import {
  mapRollout,
  mapRolloutMemberState,
  mapRolloutState,
  rolloutMemberStateToTargetPhase,
} from "@/protoFleet/api/rolloutMappers";

const timestamp = (iso: string) =>
  create(TimestampSchema, {
    seconds: BigInt(Math.floor(new Date(iso).getTime() / 1000)),
  });

describe("rollout mappers", () => {
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
});
