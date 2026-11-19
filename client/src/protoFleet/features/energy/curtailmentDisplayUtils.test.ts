// @vitest-environment node
import { describe, expect, it, vi } from "vitest";

import { create } from "@bufbuild/protobuf";
import {
  formatCurtailmentDuration,
  getActiveCurtailmentCurtailProgress,
  getActiveCurtailmentRestoreProgress,
  getCurtailmentEventScopeLabel,
  mapCurtailmentEventState,
  mapCurtailmentTargetState,
} from "@/protoFleet/api/curtailmentDisplayUtils";
import { getCurtailmentScopeSummary } from "@/protoFleet/api/curtailmentScopes";
import type { CurtailmentTargetRollup } from "@/protoFleet/api/curtailmentTypes";

import {
  CurtailmentEventSchema,
  CurtailmentEventState,
  CurtailmentScopeSchema,
  CurtailmentTargetState,
  ScopeBuildingSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";

function rollups(counts: Partial<Record<CurtailmentTargetRollup["state"], number>>): CurtailmentTargetRollup[] {
  return Object.entries(counts).map(([state, count]) => ({
    state: state as CurtailmentTargetRollup["state"],
    count: count ?? 0,
  }));
}

describe("getActiveCurtailmentCurtailProgress", () => {
  it("counts sent and confirmed targets as reached, but only confirmed toward the percent", () => {
    // Issue #660's fixture: total 500 = confirmed 300 + dispatched 120 +
    // pending 80 -> reached 420 of 500. The percent is confirmed-based per
    // #670's design pass (the card shows the telemetry-verified curtailed
    // share; in-flight work stays visible as the Curtailing segment), so the
    // exported percent matches the rendered "300 of 500 (60%)" summary.
    const progress = getActiveCurtailmentCurtailProgress({
      rollups: rollups({ confirmed: 300, dispatched: 120, pending: 80 }),
    });

    expect(progress).toEqual({
      confirmedCount: 300,
      sentCount: 120,
      driftedCount: 0,
      pendingCount: 80,
      unavailableCount: 0,
      dispatchableCount: 500,
      reachedCount: 420,
      percent: 60,
    });
  });

  it("counts drifted targets separately, never as reached", () => {
    // DRIFTED means telemetry observed the miner uncurtailed and it awaits a
    // successful redispatch — reporting it as reached would overstate live
    // curtailment during an SLA obligation.
    const progress = getActiveCurtailmentCurtailProgress({
      rollups: rollups({ confirmed: 10, dispatched: 4, drifted: 2, pending: 4 }),
    });

    expect(progress.sentCount).toBe(4);
    expect(progress.driftedCount).toBe(2);
    expect(progress.reachedCount).toBe(14);
    expect(progress.dispatchableCount).toBe(20);
    expect(progress.percent).toBe(50);
  });

  it("never reports 100% while drifted targets remain", () => {
    const progress = getActiveCurtailmentCurtailProgress({
      rollups: rollups({ confirmed: 18, drifted: 2 }),
    });

    expect(progress.reachedCount).toBe(18);
    expect(progress.dispatchableCount).toBe(20);
    expect(progress.percent).toBe(90);
  });

  it("excludes unavailable targets from the dispatchable denominator but reports them", () => {
    const progress = getActiveCurtailmentCurtailProgress({
      rollups: rollups({ confirmed: 8, pending: 2, unavailable: 5 }),
    });

    expect(progress.dispatchableCount).toBe(10);
    expect(progress.unavailableCount).toBe(5);
    expect(progress.percent).toBe(80);
  });

  it("reaches 100% when every dispatchable target is confirmed despite unavailable targets", () => {
    const progress = getActiveCurtailmentCurtailProgress({
      rollups: rollups({ confirmed: 10, unavailable: 40 }),
    });

    expect(progress.reachedCount).toBe(10);
    expect(progress.dispatchableCount).toBe(10);
    expect(progress.percent).toBe(100);
  });

  it("keeps the percent below 100 while sent targets await confirmation", () => {
    const progress = getActiveCurtailmentCurtailProgress({
      rollups: rollups({ confirmed: 9, dispatched: 1, unavailable: 40 }),
    });

    expect(progress.reachedCount).toBe(10);
    expect(progress.dispatchableCount).toBe(10);
    expect(progress.percent).toBe(90);
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
      driftedCount: 0,
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

describe("formatCurtailmentDuration", () => {
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
    expect(formatCurtailmentDuration(seconds)).toBe(expected);
  });
});

describe("getCurtailmentEventScopeLabel", () => {
  it("preserves localized event counts and unlocalized form summaries", () => {
    const buildingIds = Array.from({ length: 1234 }, (_, index) => String(index + 1));
    const event = create(CurtailmentEventSchema, {
      scopes: buildingIds.map((buildingId) =>
        create(CurtailmentScopeSchema, {
          scope: { case: "building", value: create(ScopeBuildingSchema, { buildingId: BigInt(buildingId) }) },
        }),
      ),
    });
    const localeSpy = vi.spyOn(Number.prototype, "toLocaleString").mockImplementation(function (this: number) {
      return new Intl.NumberFormat("de-DE").format(Number(this));
    });
    try {
      expect(getCurtailmentEventScopeLabel(event)).toBe("1.234 buildings");
      expect(getCurtailmentScopeSummary({ type: "building", buildingIds }, { fallbackLabel: "Whole fleet" })).toBe(
        "1234 buildings",
      );
    } finally {
      localeSpy.mockRestore();
    }
  });

  it("summarizes a typed topology terminal scope without treating it as a generic device set", () => {
    const event = create(CurtailmentEventSchema, {
      scopes: [
        create(CurtailmentScopeSchema, {
          scope: { case: "building", value: create(ScopeBuildingSchema, { buildingId: 10n }) },
        }),
        create(CurtailmentScopeSchema, {
          scope: { case: "building", value: create(ScopeBuildingSchema, { buildingId: 11n }) },
        }),
      ],
    });

    expect(getCurtailmentEventScopeLabel(event)).toBe("2 buildings");
  });
});

describe("formatCurtailmentDuration styles", () => {
  it.each([
    [0, "Immediate", "Immediately"],
    [1, "1 second", "~1 second"],
    [61, "1 minute, 1 second", "~2 minutes"],
    [3600, "60 minutes", "~1 hour"],
    [3601, "60 minutes, 1 second", "~1 hour 1 minute"],
  ] as const)("preserves long and estimated duration wording for %s seconds", (seconds, long, estimate) => {
    expect(formatCurtailmentDuration(seconds, { style: "long" })).toBe(long);
    expect(formatCurtailmentDuration(seconds, { style: "estimate" })).toBe(estimate);
  });

  it("omits approximation only when requested for infrastructure delays", () => {
    expect(formatCurtailmentDuration(61, { style: "estimate", approximate: false })).toBe("2 minutes");
  });
});

it("preserves the elapsed display's unlocalized minute and second digits", () => {
  const localeSpy = vi.spyOn(Number.prototype, "toLocaleString").mockImplementation(function (this: number) {
    return new Intl.NumberFormat("ar-EG").format(Number(this));
  });
  try {
    expect(formatCurtailmentDuration(123)).toBe("2m 3s");
    expect(formatCurtailmentDuration(3660)).toBe("١h 1m");
    expect(formatCurtailmentDuration(123, { style: "long" })).toBe("٢ minutes, ٣ seconds");
    expect(formatCurtailmentDuration(123, { style: "estimate" })).toBe("~3 minutes");
  } finally {
    localeSpy.mockRestore();
  }
});

describe("unknown curtailment states", () => {
  it.each([CurtailmentEventState.UNSPECIFIED, 999])("keeps event state %s out of pending", (state) => {
    expect(mapCurtailmentEventState(state)).toBe("unknown");
  });
  it.each([CurtailmentTargetState.UNSPECIFIED, 999])("keeps target state %s out of pending", (state) => {
    expect(mapCurtailmentTargetState(state)).toBe("unknown");
  });
  it("counts unknown targets as unavailable rather than pending", () => {
    expect(getActiveCurtailmentCurtailProgress({ rollups: [{ state: "unknown", count: 2 }] })).toMatchObject({
      pendingCount: 0,
      unavailableCount: 2,
      reachedCount: 0,
    });
  });
});
