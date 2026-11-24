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

  test("does not start any work when the signal is already aborted", async () => {
    const map = vi.fn(async (value: number) => value * 2);
    const controller = new AbortController();
    controller.abort();

    await expect(mapWithConcurrency([1, 2, 3], 2, map, controller.signal)).rejects.toMatchObject({
      name: "AbortError",
    });
    expect(map).not.toHaveBeenCalled();
  });

  test("stops launching remaining work once aborted mid-flight", async () => {
    const controller = new AbortController();
    // Single worker processes serially; abort after the first item so the
    // second and third never launch — the CT1 fan-out cancels its later waves.
    const map = vi.fn(async (value: number) => {
      controller.abort();
      return value * 2;
    });

    await expect(mapWithConcurrency([1, 2, 3], 1, map, controller.signal)).rejects.toMatchObject({
      name: "AbortError",
    });
    expect(map).toHaveBeenCalledTimes(1);
  });
});
