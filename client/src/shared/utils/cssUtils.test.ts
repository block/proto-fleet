import { describe, expect, test } from "vitest";
import { classNameToSelectors, cubicBezierValues } from "./cssUtils";

describe("cubicBezierValues", () => {
  test("should extract cubic bezier values from a css string", () => {
    const string = "cubic-bezier(0.47, 0, 0.23, 1.38)";
    const result = cubicBezierValues(string);
    expect(result).toEqual([0.47, 0, 0.23, 1.38]);
  });

  test("should extract cubic bezier values from a css string with other transition values", () => {
    const string = "opacity 1s .5s cubic-bezier(0.47, 0, 0.23, 1.38)";
    const result = cubicBezierValues(string);
    expect(result).toEqual([0.47, 0, 0.23, 1.38]);
  });

  test("should return undefined if the string does not contain a cubic-bezier", () => {
    const string = "ease-in-out(0.47, 0, 0.23, 1.38)";
    const result = cubicBezierValues(string);
    expect(result).toEqual(undefined);
  });

  test("should return undefined if there are not exactly 4 values", () => {
    const string = "cubic-bezier(0.47, 0, 0.23)";
    const result = cubicBezierValues(string);
    expect(result).toEqual(undefined);

    const string2 = "cubic-bezier(0.47, 0, 0.23, 1.38, 0.5)";
    const result2 = cubicBezierValues(string2);
    expect(result2).toEqual(undefined);
  });
});

describe("classNameToSelectors", () => {
  test("should return a selector for each class token", () => {
    expect(classNameToSelectors("schedule-pill-trigger relative")).toEqual([
      '[class~="schedule-pill-trigger"]',
      '[class~="relative"]',
    ]);
  });

  test("should ignore extra whitespace between class tokens", () => {
    expect(classNameToSelectors("  schedule-pill-trigger   relative  ")).toEqual([
      '[class~="schedule-pill-trigger"]',
      '[class~="relative"]',
    ]);
  });

  test("should escape class tokens for css attribute selectors", () => {
    expect(classNameToSelectors('before:content-["open"] path\\to\\trigger')).toEqual([
      '[class~="before:content-[\\"open\\"]"]',
      '[class~="path\\\\to\\\\trigger"]',
    ]);
  });
});
