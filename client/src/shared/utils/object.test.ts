import { describe, expect, test } from "vitest";

import { omit, pick } from "./object";

describe("pick", () => {
  test("should return new object with only specified keys", () => {
    const obj = { a: 1, b: 2, c: 3, d: 4 };
    const result = pick(obj, ["a", "c"]);
    expect(result).toEqual({ a: 1, c: 3 });
  });

  test("should return new object with only specified keys", () => {
    const obj = { a: 1, b: 2, c: 3, d: 4 };
    // @ts-ignore
    const result = pick(obj, ["a", "c", "e"]);
    expect(result).toEqual({ a: 1, c: 3 });
  });
});

describe("omit", () => {
  test("should return new object with specified keys omitted", () => {
    const obj = { a: 1, b: 2, c: 3, d: 4 };
    const result = omit(obj, ["a", "c"]);
    expect(result).toEqual({ b: 2, d: 4 });
  });

  test("should return new object with specified keys omitted", () => {
    const obj = { a: 1, b: 2, c: 3, d: 4 };
    // @ts-ignore
    const result = omit(obj, ["a", "c", "e"]);
    expect(result).toEqual({ b: 2, d: 4 });
  });
});
