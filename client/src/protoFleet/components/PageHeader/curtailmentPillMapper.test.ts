import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import { mapCurtailmentPillEvent } from "./curtailmentPillMapper";
import {
  CurtailmentEventSchema,
  CurtailmentEventState,
  CurtailmentTargetRollupSchema,
  CurtailmentTargetSchema,
  CurtailmentTargetState,
  FixedKwParamsSchema,
  ScopeWholeOrgSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import type { CurtailmentEvent } from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";

function targetRollup(values: { pending?: number; confirmed?: number; total?: number }) {
  return create(CurtailmentTargetRollupSchema, values);
}

function curtailmentEvent(overrides: Partial<CurtailmentEvent> = {}): CurtailmentEvent {
  const event = create(CurtailmentEventSchema, {
    reason: "Grid peak",
    state: CurtailmentEventState.PENDING,
    scope: {
      case: "wholeOrg",
      value: create(ScopeWholeOrgSchema, {}),
    },
    modeParams: {
      case: "fixedKw",
      value: create(FixedKwParamsSchema, { targetKw: 20 }),
    },
    targetRollup: targetRollup({ pending: 2, total: 2 }),
    decisionSnapshot: {
      estimated_reduction_kw: 23.4,
      selected_count: 2,
    },
  });

  return Object.assign(event, overrides);
}

describe("mapCurtailmentPillEvent", () => {
  it.each([
    [{}, "pending"],
    [{ targetRollup: targetRollup({ confirmed: 1, pending: 1, total: 2 }) }, "curtailing"],
    [{ state: CurtailmentEventState.ACTIVE, targetRollup: targetRollup({ confirmed: 2, total: 2 }) }, "curtailed"],
    [{ state: CurtailmentEventState.RESTORING }, "restoring"],
  ] satisfies readonly [Partial<CurtailmentEvent>, string][])("maps display state", (overrides, state) => {
    expect(mapCurtailmentPillEvent(curtailmentEvent(overrides))).toEqual(
      expect.objectContaining({
        state,
        targetMetricsAvailable: true,
      }),
    );
  });

  it("prefers the live rollup total over the snapshot count", () => {
    expect(
      mapCurtailmentPillEvent(
        curtailmentEvent({
          state: CurtailmentEventState.ACTIVE,
          decisionSnapshot: {
            estimated_reduction_kw: 23.4,
            selected_count: 10,
          },
          targetRollup: targetRollup({ confirmed: 4000, pending: 1000, total: 5000 }),
        }),
      ),
    ).toEqual(
      expect.objectContaining({
        selectedMiners: 5000,
        estimatedReductionAvailable: true,
      }),
    );
  });

  it("treats a zeroed live rollup as zero targets even when a snapshot count exists", () => {
    expect(
      mapCurtailmentPillEvent(
        curtailmentEvent({
          state: CurtailmentEventState.ACTIVE,
          decisionSnapshot: {
            estimated_reduction_kw: 23.4,
            selected_count: 10,
          },
          targetRollup: create(CurtailmentTargetRollupSchema, {}),
        }),
      ),
    ).toEqual(
      expect.objectContaining({
        selectedMiners: 0,
        targetMetricsAvailable: true,
      }),
    );
  });

  it("falls back to hydrated targets, then the snapshot count, when no rollup exists", () => {
    expect(
      mapCurtailmentPillEvent(
        curtailmentEvent({
          state: CurtailmentEventState.ACTIVE,
          decisionSnapshot: {
            estimated_reduction_kw: 23.4,
            selected_count: 10,
          },
          targetRollup: undefined,
          targets: [
            create(CurtailmentTargetSchema, { deviceIdentifier: "miner-1", state: CurtailmentTargetState.CONFIRMED }),
            create(CurtailmentTargetSchema, { deviceIdentifier: "miner-2", state: CurtailmentTargetState.CONFIRMED }),
            create(CurtailmentTargetSchema, { deviceIdentifier: "miner-3", state: CurtailmentTargetState.PENDING }),
          ],
        }),
      ),
    ).toEqual(
      expect.objectContaining({
        selectedMiners: 3,
      }),
    );

    expect(
      mapCurtailmentPillEvent(
        curtailmentEvent({
          state: CurtailmentEventState.ACTIVE,
          decisionSnapshot: {
            estimated_reduction_kw: 23.4,
            selected_count: 10,
          },
          targetRollup: undefined,
          targets: [],
        }),
      ),
    ).toEqual(
      expect.objectContaining({
        selectedMiners: 10,
      }),
    );
  });

  it("does not treat baseline-less targets as proof of a kW estimate", () => {
    // baseline_power_w is optional (telemetry gap at selection); without a
    // snapshot estimate, baseline-less targets would sum to a fabricated
    // 0.0 kW, so the estimate must report unavailable.
    const baselessTargets = [
      create(CurtailmentTargetSchema, { deviceIdentifier: "miner-1", state: CurtailmentTargetState.CONFIRMED }),
      create(CurtailmentTargetSchema, { deviceIdentifier: "miner-2", state: CurtailmentTargetState.PENDING }),
    ];
    expect(
      mapCurtailmentPillEvent(
        curtailmentEvent({
          state: CurtailmentEventState.ACTIVE,
          decisionSnapshot: {},
          targetRollup: undefined,
          targets: baselessTargets,
        }),
      ),
    ).toEqual(
      expect.objectContaining({
        selectedMiners: 2,
        estimatedReductionAvailable: false,
      }),
    );

    expect(
      mapCurtailmentPillEvent(
        curtailmentEvent({
          state: CurtailmentEventState.ACTIVE,
          decisionSnapshot: {},
          targetRollup: undefined,
          targets: [
            ...baselessTargets,
            create(CurtailmentTargetSchema, {
              deviceIdentifier: "miner-3",
              state: CurtailmentTargetState.CONFIRMED,
              baselinePowerW: 3000,
            }),
          ],
        }),
      ),
    ).toEqual(
      expect.objectContaining({
        selectedMiners: 3,
        estimatedReductionAvailable: true,
      }),
    );
  });

  it("marks rollup-only summary rows as counts-only so no zero estimate renders", () => {
    expect(
      mapCurtailmentPillEvent(
        curtailmentEvent({
          state: CurtailmentEventState.ACTIVE,
          decisionSnapshot: {},
          targetRollup: targetRollup({ confirmed: 4000, pending: 1000, total: 5000 }),
          targets: [],
        }),
      ),
    ).toEqual(
      expect.objectContaining({
        selectedMiners: 5000,
        targetMetricsAvailable: true,
        estimatedReductionAvailable: false,
        estimatedReductionKw: 0,
      }),
    );
  });

  it("marks summary-only active rows as missing target metrics", () => {
    expect(
      mapCurtailmentPillEvent(
        curtailmentEvent({
          decisionSnapshot: {},
          targetRollup: undefined,
          targets: [],
        }),
      ),
    ).toEqual(
      expect.objectContaining({
        state: "pending",
        targetMetricsAvailable: false,
      }),
    );
  });

  it("falls back for blank reasons and hides inactive terminal events", () => {
    expect(mapCurtailmentPillEvent(curtailmentEvent({ reason: "" }))).toEqual(
      expect.objectContaining({
        reason: "Curtailment",
      }),
    );

    expect(mapCurtailmentPillEvent(curtailmentEvent({ state: CurtailmentEventState.COMPLETED }))).toBeNull();
  });
});
