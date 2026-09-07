import { describe, expect, it } from "vitest";

import { foldAsciiCase, minerTargetKey } from "./minerTarget";

describe("minerTargetKey", () => {
  it("matches the server: trims and folds ASCII letters only", () => {
    expect(minerTargetKey(" Bitmain ", "S21")).toBe(minerTargetKey("bitmain", "s21"));
    expect(foldAsciiCase("Kelvin")).toBe("kelvin");
    // U+212A KELVIN SIGN lowercases to "k" under Unicode rules; the server does
    // not fold it, so neither does the client.
    expect(minerTargetKey("\u212Aelvin", "S21")).not.toBe(minerTargetKey("Kelvin", "S21"));
    expect(minerTargetKey("Bít main", "S21")).toBe(minerTargetKey("Bít main", "S21"));
    expect(minerTargetKey("BÍT main", "S21")).not.toBe(minerTargetKey("bít main", "S21"));
  });

  it("never matches an absent target", () => {
    expect(minerTargetKey(undefined, "S21")).toBeNull();
    expect(minerTargetKey("Bitmain", "  ")).toBeNull();
  });
});
