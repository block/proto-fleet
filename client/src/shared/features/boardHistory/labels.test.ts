import { describe, expect, it } from "vitest";

import { eventLabel } from "./labels";

describe("eventLabel", () => {
  it("reads the controller's unspecified enum values as unknown, like missing ones", () => {
    const time = "2026-11-15T10:00:00Z";
    expect(eventLabel({ id: "1", kind: "error", time, cause: "HASHBOARD_ERROR_CAUSE_UNSPECIFIED" })).toBe(
      "Error: Unknown error",
    );
    expect(eventLabel({ id: "2", kind: "warning", time })).toBe("Warning: Unknown error");
    expect(
      eventLabel({
        id: "3",
        kind: "stateChange",
        time,
        fromState: "MINING_STATE_UNSPECIFIED",
        toState: "MINING_STATE_MINING",
      }),
    ).toBe("Mining state Unknown → Mining");
  });
});
