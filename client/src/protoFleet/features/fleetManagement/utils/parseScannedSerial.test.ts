import { describe, expect, it } from "vitest";

import { parseScannedSerial } from "./parseScannedSerial";

describe("parseScannedSerial", () => {
  it("strips the SN: prefix from the sample QR payload", () => {
    expect(parseScannedSerial("SN:1234567890123456")).toBe("1234567890123456");
  });

  it("handles a bare serial with no prefix", () => {
    expect(parseScannedSerial("1234567890123456")).toBe("1234567890123456");
  });

  it.each([
    ["SN:ABC123", "ABC123"],
    ["sn:abc123", "abc123"],
    ["S/N:ABC123", "ABC123"],
    ["S/N ABC123", "ABC123"],
    ["SN=ABC123", "ABC123"],
    ["SN ABC123", "ABC123"],
    ["SN#ABC123", "ABC123"],
    ["SERIAL:ABC123", "ABC123"],
    ["Serial No: ABC123", "ABC123"],
    ["SERIAL NUMBER: ABC123", "ABC123"],
  ])("normalizes prefix variant %s", (input, expected) => {
    expect(parseScannedSerial(input)).toBe(expected);
  });

  it("trims surrounding whitespace and trailing newline from scanners", () => {
    expect(parseScannedSerial("  SN:ABC123  \n")).toBe("ABC123");
    expect(parseScannedSerial("SN:ABC123\r\n")).toBe("ABC123");
  });

  it("takes only the first line of a multi-line payload", () => {
    expect(parseScannedSerial("SN:ABC123\nMODEL:S21")).toBe("ABC123");
  });

  it("does not mangle a serial that legitimately starts with SN letters", () => {
    // No separator after "SN", so it is part of the value, not a prefix.
    expect(parseScannedSerial("SNX9000")).toBe("SNX9000");
  });

  it("returns empty string for empty or whitespace-only input", () => {
    expect(parseScannedSerial("")).toBe("");
    expect(parseScannedSerial("   ")).toBe("");
    expect(parseScannedSerial("SN:")).toBe("");
  });
});
