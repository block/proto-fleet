import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import { mapCurtailmentPillEvent } from "./curtailmentPillMapper";
import {
  type CurtailmentEvent,
  CurtailmentEventSchema,
  CurtailmentEventState,
  CurtailmentMode,
  CurtailmentPriority,
  CurtailmentTargetRollupSchema,
  FixedKwParamsSchema,
  ScopeWholeOrgSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";

function curtailmentEvent(overrides: Partial<CurtailmentEvent> = {}): CurtailmentEvent {
  const event = create(CurtailmentEventSchema, {
    eventUuid: "curt-1",
    reason: "Grid peak",
    state: CurtailmentEventState.PENDING,
    mode: CurtailmentMode.FIXED_KW,
    priority: CurtailmentPriority.NORMAL,
    scope: {
      case: "wholeOrg",
      value: create(ScopeWholeOrgSchema, {}),
    },
    modeParams: {
      case: "fixedKw",
      value: create(FixedKwParamsSchema, { targetKw: 20 }),
    },
    targetRollup: create(CurtailmentTargetRollupSchema, {
      pending: 2,
      total: 2,
    }),
    decisionSnapshot: {
      estimated_reduction_kw: 23.4,
      selected_count: 2,
    },
  });

  return Object.assign(event, overrides);
}

describe("mapCurtailmentPillEvent", () => {
  it("keeps a fully pending active event pending", () => {
    expect(mapCurtailmentPillEvent(curtailmentEvent())).toEqual(
      expect.objectContaining({
        state: "pending",
      }),
    );
  });

  it("shows a pending event with started targets as curtailing", () => {
    const event = curtailmentEvent({
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 1,
        pending: 1,
        total: 2,
      }),
    });

    expect(mapCurtailmentPillEvent(event)).toEqual(
      expect.objectContaining({
        state: "curtailing",
      }),
    );
  });

  it("keeps a pending event with all targets confirmed as curtailing", () => {
    const event = curtailmentEvent({
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 2,
        total: 2,
      }),
    });

    expect(mapCurtailmentPillEvent(event)).toEqual(
      expect.objectContaining({
        state: "curtailing",
      }),
    );
  });

  it("shows an active event with remaining targets as curtailing", () => {
    const event = curtailmentEvent({
      state: CurtailmentEventState.ACTIVE,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 1,
        pending: 1,
        total: 2,
      }),
    });

    expect(mapCurtailmentPillEvent(event)).toEqual(
      expect.objectContaining({
        state: "curtailing",
      }),
    );
  });

  it("shows an active event with all targets confirmed as curtailed", () => {
    const event = curtailmentEvent({
      state: CurtailmentEventState.ACTIVE,
      targetRollup: create(CurtailmentTargetRollupSchema, {
        confirmed: 2,
        total: 2,
      }),
    });

    expect(mapCurtailmentPillEvent(event)).toEqual(
      expect.objectContaining({
        state: "curtailed",
      }),
    );
  });

  it("passes through restoring events as restoring", () => {
    expect(mapCurtailmentPillEvent(curtailmentEvent({ state: CurtailmentEventState.RESTORING }))).toEqual(
      expect.objectContaining({
        state: "restoring",
      }),
    );
  });

  it("hides inactive terminal events", () => {
    expect(mapCurtailmentPillEvent(curtailmentEvent({ state: CurtailmentEventState.COMPLETED }))).toBeNull();
  });
});
