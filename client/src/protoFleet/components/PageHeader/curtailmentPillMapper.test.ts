import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import { mapCurtailmentPillEvent } from "./curtailmentPillMapper";
import {
  CurtailmentEventSchema,
  CurtailmentEventState,
  CurtailmentTargetRollupSchema,
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
