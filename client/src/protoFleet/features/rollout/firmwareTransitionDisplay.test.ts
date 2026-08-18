import { describe, expect, it } from "vitest";

import { firmwareTransitionDisplay } from "./firmwareTransitionDisplay";

describe("firmware transition display", () => {
  it("keeps sentence-case row labels separate from grammatical count labels", () => {
    expect(firmwareTransitionDisplay.updating).toMatchObject({
      tableLabel: "Updating firmware",
      countLabel: "updating firmware",
      status: "error",
    });
    expect(firmwareTransitionDisplay.verifying).toMatchObject({
      tableLabel: "Verifying firmware",
      countLabel: "verifying firmware",
      status: "warning",
    });
    expect(firmwareTransitionDisplay.needsAttention).toMatchObject({
      tableLabel: "Needs attention",
      countLabel: "needs attention",
      status: "error",
    });
  });
});
