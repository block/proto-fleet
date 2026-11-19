// @vitest-environment node
import { describe, expect, it } from "vitest";
import { isDuration, isFleetDuration } from "./constants";

describe("DurationSelector constants", () => {
  describe("isDuration", () => {
    it("returns true for valid durations", () => {
      expect(isDuration("1h")).toBe(true);
      expect(isDuration("5d")).toBe(true);
    });

    it("returns false for invalid values", () => {
      expect(isDuration("2h")).toBe(false);
      expect(isDuration("invalid")).toBe(false);
      expect(isDuration("3d")).toBe(false);
      expect(isDuration("__proto__")).toBe(false);
      expect(isDuration("constructor")).toBe(false);
      expect(isDuration(null)).toBe(false);
    });
  });

  describe("isFleetDuration", () => {
    it("returns true for valid Fleet durations", () => {
      expect(isFleetDuration("1h")).toBe(true);
      expect(isFleetDuration("7d")).toBe(true);
      expect(isFleetDuration("1y")).toBe(true);
    });

    it("returns false for invalid Fleet duration values", () => {
      expect(isFleetDuration("12h")).toBe(false);
      expect(isFleetDuration("3d")).toBe(false);
      expect(isFleetDuration("10d")).toBe(false);
      expect(isFleetDuration("invalid")).toBe(false);
      expect(isFleetDuration("__proto__")).toBe(false);
      expect(isFleetDuration("constructor")).toBe(false);
      expect(isFleetDuration(null)).toBe(false);
    });
  });
});
