import { describe, expect, test } from "vitest";

import { arrayOfWarnings } from "./utility";

describe("getTickValue", () => {
  test("should have length of passed number", () => {
    expect(arrayOfWarnings(7).length).toBe(7);
  });

  test("should limit length to 8", () => {
    expect(arrayOfWarnings(9).length).toBe(8);
  });
});
