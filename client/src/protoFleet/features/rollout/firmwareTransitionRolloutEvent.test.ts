import { describe, expect, it } from "vitest";

import { mapFirmwareTransitionToRolloutEvent } from "./firmwareTransitionRolloutEvent";
import type { FirmwareTransitionProgress } from "./rolloutTypes";

function progress(overrides: Partial<FirmwareTransitionProgress> = {}): FirmwareTransitionProgress {
  return {
    totalCount: 3,
    pendingCount: 1,
    updatingCount: 1,
    verifyingCount: 0,
    confirmedCount: 1,
    attentionCount: 0,
    members: [],
    ...overrides,
  };
}

describe("mapFirmwareTransitionToRolloutEvent", () => {
  it("maps active enforcement to an all-at-once firmware rollout", () => {
    const event = mapFirmwareTransitionToRolloutEvent(progress(), {
      scopeLabel: "Stable production",
      startedAt: "2026-08-18T12:00:00.000Z",
    });

    expect(event).toMatchObject({
      processType: "firmware",
      state: "inProgress",
      title: "Initial firmware rollout",
      scopeLabel: "Stable production",
      strategy: "allAtOnce",
      startedAt: "2026-08-18T12:00:00.000Z",
      totalTargets: 3,
      excludedTargets: 0,
      convergenceProgress: {
        completed: 1,
        total: 3,
        attentionRequired: 0,
      },
      rollups: [
        { phase: "done", count: 1 },
        { phase: "inProgress", count: 1 },
        { phase: "queued", count: 1 },
      ],
    });
  });

  it("maps a fully confirmed transition to completed", () => {
    const event = mapFirmwareTransitionToRolloutEvent(
      progress({
        pendingCount: 0,
        updatingCount: 0,
        confirmedCount: 3,
      }),
      { scopeLabel: "Stable production" },
    );

    expect(event.state).toBe("completed");
    expect(event.convergenceProgress).toEqual({
      completed: 3,
      total: 3,
      attentionRequired: 0,
    });
  });

  it("keeps verifying miners in progress", () => {
    const event = mapFirmwareTransitionToRolloutEvent(
      progress({
        pendingCount: 0,
        updatingCount: 0,
        verifyingCount: 2,
        confirmedCount: 1,
      }),
      { scopeLabel: "Stable production" },
    );

    expect(event.state).toBe("inProgress");
    expect(event.rollups).toContainEqual({ phase: "inProgress", count: 2 });
  });

  it("maps terminal attention to completed with failures and member errors", () => {
    const event = mapFirmwareTransitionToRolloutEvent(
      progress({
        totalCount: 2,
        pendingCount: 0,
        updatingCount: 0,
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
      }),
      { scopeLabel: "Stable production" },
    );

    expect(event.state).toBe("completedWithFailures");
    expect(event.convergenceProgress).toEqual({
      completed: 2,
      total: 2,
      attentionRequired: 1,
    });
    expect(event.rollups).toContainEqual({ phase: "attentionRequired", count: 1 });
    expect(event.errors).toEqual([
      {
        id: "initial-firmware-error-1",
        message: "Firmware identity could not be confirmed",
        impactedMiners: ["miner-2"],
      },
    ]);
  });
});
