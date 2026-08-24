import { describe, expect, it } from "vitest";

import { isRolloutGroupResultAcknowledged, rolloutGroupAcknowledgement } from "./rolloutResultAcknowledgement";
import type { RolloutGroup } from "./rolloutTypes";

function group(resultRevision: bigint, resultReady: boolean): RolloutGroup {
  return {
    id: "parent-1",
    laneId: "lane-1",
    name: "Two model rollout",
    reason: "Update both models",
    resultRevision,
    terminalOutcome: "successful",
    resultReady,
    lifecycle: "terminal",
    activity: "settled",
    needsAction: false,
    evidenceReadiness: resultReady ? "ready" : "pending",
    models: [],
    children: [],
  };
}

describe("aggregate rollout result acknowledgement", () => {
  it("stores only a ready parent ID and result revision", () => {
    expect(rolloutGroupAcknowledgement(group(1n, false))).toBeUndefined();
    expect(rolloutGroupAcknowledgement(group(1n, true))).toEqual({
      parentId: "parent-1",
      resultRevision: "1",
    });
  });

  it("resurfaces a ready result when its revision changes", () => {
    const acknowledged = rolloutGroupAcknowledgement(group(1n, true));

    expect(isRolloutGroupResultAcknowledged(group(1n, true), acknowledged)).toBe(true);
    expect(isRolloutGroupResultAcknowledged(group(2n, true), acknowledged)).toBe(false);
  });
});
