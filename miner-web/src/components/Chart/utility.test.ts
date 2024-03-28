import { describe, expect, test } from "vitest";

import { getTickValue } from "./utility";

describe("getTickValue", () => {
  test("should round down to 2 decimal places", () => {
    expect(getTickValue(1.234)).toBe(1.23);
  });

  test("should round up to 2 decimal places", () => {
    expect(getTickValue(1.235)).toBe(1.24);
  });

  test("should remove margin value", () => {
    expect(getTickValue(1.234, 0.01)).toBe(1.22);
  });
});
