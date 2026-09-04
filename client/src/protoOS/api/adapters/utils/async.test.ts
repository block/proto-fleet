import { describe, expect, test, vi } from "vitest";

import { mapWithConcurrency } from "./async";

describe("mapWithConcurrency", () => {
  test("maps values with one worker when the requested limit is not positive", async () => {
    const map = vi.fn(async (value: number) => value * 2);

    await expect(mapWithConcurrency([1, 2, 3], 0, map)).resolves.toEqual([2, 4, 6]);
    expect(map).toHaveBeenCalledTimes(3);
  });

  test("returns an empty result without starting workers", async () => {
    const map = vi.fn(async (value: number) => value * 2);

    await expect(mapWithConcurrency([], -1, map)).resolves.toEqual([]);
    expect(map).not.toHaveBeenCalled();
  });
});
