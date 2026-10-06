import { describe, expect, it } from "vitest";

import { foldAsciiCase, minerTargetKey, trimMinerTarget } from "./minerTarget";

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

  it("matches Go whitespace trimming without erasing BOMs or internal whitespace", () => {
    const whitespace =
      "\u0009\u000A\u000B\u000C\u000D\u0020\u0085\u00A0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A\u2028\u2029\u202F\u205F\u3000";
    expect(minerTargetKey(`${whitespace}Proto${whitespace}`, `${whitespace}Rig${whitespace}`)).toBe(
      minerTargetKey("proto", "rig"),
    );
    expect(minerTargetKey(whitespace, "Rig")).toBeNull();
    expect(minerTargetKey("Proto", whitespace)).toBeNull();
    expect(trimMinerTarget(" \uFEFFProto\uFEFF ")).toBe("\uFEFFProto\uFEFF");
    expect(minerTargetKey("\uFEFFProto", "Rig")).not.toBe(minerTargetKey("Proto", "Rig"));
    expect(minerTargetKey("Proto", "Rig\uFEFF")).not.toBe(minerTargetKey("Proto", "Rig"));
    expect(trimMinerTarget(" A\t\nB ")).toBe("A\t\nB");
  });

  it("keeps control characters within their own identity component", () => {
    expect(minerTargetKey("a\u0000b", "c")).not.toBe(minerTargetKey("a", "b\u0000c"));
  });
});
