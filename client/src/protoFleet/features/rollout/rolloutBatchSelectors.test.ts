import { describe, expect, it } from "vitest";

import { latestCompletedRolloutBatch } from "./rolloutBatchSelectors";
import type { RolloutBatch } from "./rolloutTypes";

function batch(id: bigint, position: number, state: RolloutBatch["state"] = "completed"): RolloutBatch {
  return {
    id,
    position,
    label: `Batch ${id}`,
    state,
    revision: 1n,
    members: [],
  };
}

describe("latestCompletedRolloutBatch", () => {
  it("selects the highest completed position from an unsorted batch list", () => {
    const latest = batch(3n, 2);

    expect(
      latestCompletedRolloutBatch({
        batches: [latest, batch(1n, 0), batch(4n, 3, "pending"), batch(2n, 1)],
      }),
    ).toBe(latest);
  });

  it("breaks tied completed positions by the higher batch ID", () => {
    const latest = batch(9n, 2);

    expect(latestCompletedRolloutBatch({ batches: [batch(8n, 2), latest] })).toBe(latest);
  });
});
