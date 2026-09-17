import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import { planReadout } from "./behaviorUtils";
import { RolloutBehaviorSchema, RolloutMethod } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

describe("delegated rollout plan readout", () => {
  const delegated = create(RolloutBehaviorSchema, { method: RolloutMethod.DELEGATED });

  it.each([
    { count: 1, scope: "1 miner in scope" },
    { count: 48, scope: "48 miners in scope" },
  ])("describes external selection with $scope", ({ count, scope }) => {
    expect(planReadout(delegated, count)).toBe(`${scope}; an external controller decides which miners update and when`);
  });

  it("does not present a plan for an empty channel", () => {
    expect(planReadout(delegated, 0)).toBeNull();
  });
});
