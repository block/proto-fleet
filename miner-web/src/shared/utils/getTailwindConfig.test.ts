import { describe, expect, test } from "vitest";
import getTailwindConfig from "./getTailwindConfig";

describe("getTailwindConfig", () => {
  test("should return the value at the given path for default config", () => {
    const result = getTailwindConfig("theme", "transitionProperty", "opacity");
    expect(result).toEqual("opacity");
  });

  test("should return the value at the given path for default config with an array path", () => {
    const result = getTailwindConfig("darkMode", 1);
    expect(result).toEqual("[data-theme=\"dark\"]");
  });

  test("should return the value at the given path for user config", () => {
    const result = getTailwindConfig("theme", "transitionTimingFunction", "gentle");
    expect(result).toEqual("cubic-bezier(0.47, 0, 0.23, 1.38)");
  });

  test("should return undefined if the path does not exist", () => {
    const result = getTailwindConfig("theme", "transitionTimingFunction", "doesNotExist");
    expect(result).toEqual(undefined);
  });
});