import { describe, expect, it } from "vitest";

import { reorderScheduleIdsByDrop } from "./constants";

describe("reorderScheduleIdsByDrop", () => {
  it("saves the user-arranged sorted order as the new priority order", () => {
    expect(
      reorderScheduleIdsByDrop({
        activeId: "alpha",
        overId: "charlie",
        visibleItemKeys: ["alpha", "bravo", "charlie"],
        priorityOrderedIds: ["charlie", "alpha", "bravo"],
      }),
    ).toEqual(["bravo", "charlie", "alpha"]);
  });

  it("moves only the dragged row while preserving hidden rows during filtered reordering", () => {
    expect(
      reorderScheduleIdsByDrop({
        activeId: "delta",
        overId: "beta",
        visibleItemKeys: ["beta", "delta"],
        priorityOrderedIds: ["alpha", "beta", "gamma", "delta", "epsilon"],
      }),
    ).toEqual(["alpha", "delta", "gamma", "beta", "epsilon"]);
  });

  it("merges filtered reorders from the user-visible sorted order into the saved priority order", () => {
    expect(
      reorderScheduleIdsByDrop({
        activeId: "alpha",
        overId: "delta",
        visibleItemKeys: ["alpha", "charlie", "delta"],
        priorityOrderedIds: ["charlie", "alpha", "bravo", "delta"],
      }),
    ).toEqual(["charlie", "delta", "bravo", "alpha"]);
  });

  it("returns null for invalid or no-op drops", () => {
    expect(
      reorderScheduleIdsByDrop({
        activeId: "beta",
        overId: "beta",
        visibleItemKeys: ["alpha", "beta"],
        priorityOrderedIds: ["alpha", "beta"],
      }),
    ).toBeNull();

    expect(
      reorderScheduleIdsByDrop({
        activeId: "missing",
        overId: "beta",
        visibleItemKeys: ["alpha", "beta"],
        priorityOrderedIds: ["alpha", "beta"],
      }),
    ).toBeNull();
  });
});
