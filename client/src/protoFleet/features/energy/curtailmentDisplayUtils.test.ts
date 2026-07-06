import { describe, expect, it } from "vitest";

import {
  type CurtailmentTargetRollup,
  formatCurtailmentElapsedDuration,
  getActiveCurtailmentCurtailProgress,
  getActiveCurtailmentRestoreProgress,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";

function rollups(counts: Partial<Record<CurtailmentTargetRollup["state"], number>>): CurtailmentTargetRollup[] {
  return Object.entries(counts).map(([state, count]) => ({
    state: state as CurtailmentTargetRollup["state"],
    count: count ?? 0,
  }));
}

describe("getActiveCurtailmentCurtailProgress", () => {
  it("counts sent and confirmed targets as reached out of the dispatchable total", () => {
    // Issue #660 acceptance case: total 500 = confirmed 300 + dispatched 120
    // + pending 80 -> reached 420 of 500 (84%).
    const progress = getActiveCurtailmentCurtailProgress({
      rollups: rollups({ confirmed: 300, dispatched: 120, pending: 80 }),
    });

    expect(progress).toEqual({
      confirmedCount: 300,
      sentCount: 120,
      pendingCount: 80,
      unavailableCount: 0,
      dispatchableCount: 500,
      reachedCount: 420,
      percent: 84,
    });
  });

  it("treats drifted targets as sent-awaiting-confirmation", () => {
    const progress = getActiveCurtailmentCurtailProgress({
      rollups: rollups({ confirmed: 10, dispatched: 4, drifted: 2, pending: 4 }),
    });

    expect(progress.sentCount).toBe(6);
    expect(progress.reachedCount).toBe(16);
    expect(progress.dispatchableCount).toBe(20);
    expect(progress.percent).toBe(80);
  });

  it("excludes unavailable targets from the dispatchable denominator but reports them", () => {
    const progress = getActiveCurtailmentCurtailProgress({
      rollups: rollups({ confirmed: 8, pending: 2, unavailable: 5 }),
    });

    expect(progress.dispatchableCount).toBe(10);
    expect(progress.unavailableCount).toBe(5);
    expect(progress.percent).toBe(80);
  });

  it("reaches 100% when every dispatchable target is sent or confirmed despite unavailable targets", () => {
    const progress = getActiveCurtailmentCurtailProgress({
      rollups: rollups({ confirmed: 9, dispatched: 1, unavailable: 40 }),
    });

    expect(progress.reachedCount).toBe(10);
    expect(progress.dispatchableCount).toBe(10);
    expect(progress.percent).toBe(100);
  });

  it("excludes released and resolved targets, which are no longer curtail-targeted", () => {
    const progress = getActiveCurtailmentCurtailProgress({
      rollups: rollups({ confirmed: 5, pending: 5, released: 3, resolved: 2 }),
    });

    expect(progress.dispatchableCount).toBe(10);
    expect(progress.reachedCount).toBe(5);
    expect(progress.percent).toBe(50);
  });

  it("lowers the percentage when the live target set grows mid-event", () => {
    const before = getActiveCurtailmentCurtailProgress({
      rollups: rollups({ confirmed: 98, pending: 2 }),
    });
    // All-paired claim adds 50 fresh pending targets.
    const after = getActiveCurtailmentCurtailProgress({
      rollups: rollups({ confirmed: 98, pending: 52 }),
    });

    expect(before.percent).toBe(98);
    expect(after.percent).toBe(65);
    expect(after.dispatchableCount).toBe(150);
  });

  it("returns a zeroed shape for empty rollups so callers can hide the section", () => {
    expect(getActiveCurtailmentCurtailProgress({ rollups: [] })).toEqual({
      confirmedCount: 0,
      sentCount: 0,
      pendingCount: 0,
      unavailableCount: 0,
      dispatchableCount: 0,
      reachedCount: 0,
      percent: 0,
    });
  });
});

describe("getActiveCurtailmentRestoreProgress", () => {
  it("counts resolved and released targets as restored out of the restorable total", () => {
    const progress = getActiveCurtailmentRestoreProgress({
      rollups: rollups({ resolved: 8, released: 2, confirmed: 6, pending: 2 }),
    });

    expect(progress).toEqual({
      restoredCount: 10,
      failedCount: 0,
      awaitingCount: 8,
      unavailableCount: 0,
      restorableCount: 18,
      percent: 55,
    });
  });

  it("treats in-flight restore dispatches as awaiting", () => {
    const progress = getActiveCurtailmentRestoreProgress({
      rollups: rollups({ resolved: 5, dispatched: 3, drifted: 1, confirmed: 1 }),
    });

    expect(progress.awaitingCount).toBe(5);
    expect(progress.restorableCount).toBe(10);
    expect(progress.percent).toBe(50);
  });

  it("reports restore failures separately without counting them as restored", () => {
    const progress = getActiveCurtailmentRestoreProgress({
      rollups: rollups({ resolved: 17, restoreFailed: 1 }),
    });

    expect(progress.restoredCount).toBe(17);
    expect(progress.failedCount).toBe(1);
    expect(progress.restorableCount).toBe(18);
    expect(progress.percent).toBe(94);
  });

  it("excludes unavailable targets from the restorable denominator but reports them", () => {
    const progress = getActiveCurtailmentRestoreProgress({
      rollups: rollups({ resolved: 9, pending: 1, unavailable: 5 }),
    });

    expect(progress.restorableCount).toBe(10);
    expect(progress.unavailableCount).toBe(5);
    expect(progress.percent).toBe(90);
  });

  it("floors the percentage so completion is never overstated", () => {
    const progress = getActiveCurtailmentRestoreProgress({
      rollups: rollups({ resolved: 997, pending: 3 }),
    });

    expect(progress.percent).toBe(99);
  });

  it("returns a zeroed shape for empty rollups so callers can hide the section", () => {
    expect(getActiveCurtailmentRestoreProgress({ rollups: [] })).toEqual({
      restoredCount: 0,
      failedCount: 0,
      awaitingCount: 0,
      unavailableCount: 0,
      restorableCount: 0,
      percent: 0,
    });
  });
});

describe("formatCurtailmentElapsedDuration", () => {
  it.each([
    [0, "0s"],
    [45, "45s"],
    [60, "1m"],
    [192, "3m 12s"],
    [180, "3m"],
    [3900, "1h 5m"],
    [3600, "1h"],
    [-5, "0s"],
    [Number.NaN, "0s"],
  ])("formats %s seconds as %s", (seconds, expected) => {
    expect(formatCurtailmentElapsedDuration(seconds)).toBe(expected);
  });
});
