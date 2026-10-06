import { describe, expect, it } from "vitest";

import { parseScannedIdentifier } from "./parseScannedIdentifier";
import { MinerIdentifierType } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

const { UNSPECIFIED, MAC_ADDRESS, SERIAL_NUMBER } = MinerIdentifierType;

describe("parseScannedIdentifier", () => {
  describe("serial payloads", () => {
    it("strips the SN: prefix from the sample QR payload", () => {
      expect(parseScannedIdentifier("SN:1234567890123456")).toEqual({
        value: "1234567890123456",
        type: SERIAL_NUMBER,
      });
    });

    it.each([
      ["sn:abc123", "abc123"],
      ["S/N:abc123", "abc123"],
      ["S/N abc123", "abc123"],
      ["SN=abc123", "abc123"],
      ["SN#abc123", "abc123"],
      ["SERIAL:abc123", "abc123"],
      ["Serial No: abc123", "abc123"],
      ["SERIAL NUMBER: abc123", "abc123"],
    ])("normalizes serial prefix variant %s", (input, expected) => {
      expect(parseScannedIdentifier(input)).toEqual({ value: expected, type: SERIAL_NUMBER });
    });

    it("infers serial for a prefix-less non-MAC value", () => {
      expect(parseScannedIdentifier("1234567890123456")).toEqual({
        value: "1234567890123456",
        type: UNSPECIFIED,
      });
    });

    it("does not mangle a serial that legitimately starts with SN letters", () => {
      expect(parseScannedIdentifier("SNX9000")).toEqual({ value: "SNX9000", type: UNSPECIFIED });
    });
  });

  describe("MAC payloads", () => {
    it("strips the MAC: prefix", () => {
      expect(parseScannedIdentifier("MAC:00:1A:2B:3C:4D:5E")).toEqual({
        value: "00:1A:2B:3C:4D:5E",
        type: MAC_ADDRESS,
      });
    });

    it.each([
      ["MAC ADDRESS: 00:1A:2B:3C:4D:5E", "00:1A:2B:3C:4D:5E"],
      ["mac=00-1a-2b-3c-4d-5e", "00-1a-2b-3c-4d-5e"],
      ["MAC 001A2B3C4D5E", "001A2B3C4D5E"],
    ])("normalizes MAC prefix variant %s", (input, expected) => {
      expect(parseScannedIdentifier(input)).toEqual({ value: expected, type: MAC_ADDRESS });
    });

    it("infers MAC for a prefix-less colon-separated MAC", () => {
      expect(parseScannedIdentifier("00:1A:2B:3C:4D:5E")).toEqual({
        value: "00:1A:2B:3C:4D:5E",
        type: MAC_ADDRESS,
      });
    });

    it("infers MAC for a prefix-less dash-separated MAC", () => {
      expect(parseScannedIdentifier("00-1A-2B-3C-4D-5E")).toEqual({
        value: "00-1A-2B-3C-4D-5E",
        type: MAC_ADDRESS,
      });
    });

    it("infers MAC for prefix-less bare 12 hex digits", () => {
      expect(parseScannedIdentifier("001A2B3C4D5E")).toEqual({ value: "001A2B3C4D5E", type: MAC_ADDRESS });
    });

    it("does not treat a 16-digit serial as a MAC", () => {
      // 16 chars, not 12 — must not match the bare-hex MAC pattern.
      expect(parseScannedIdentifier("1234567890123456").type).toBe(UNSPECIFIED);
    });
  });

  describe("whitespace + edge cases", () => {
    it("trims surrounding whitespace and trailing newline", () => {
      expect(parseScannedIdentifier("  SN:ABC123  \n")).toEqual({ value: "ABC123", type: SERIAL_NUMBER });
      expect(parseScannedIdentifier("MAC:00:1A:2B:3C:4D:5E\r\n")).toEqual({
        value: "00:1A:2B:3C:4D:5E",
        type: MAC_ADDRESS,
      });
    });

    it("takes only the first line of a multi-line payload", () => {
      expect(parseScannedIdentifier("SN:ABC123\nMODEL:S21")).toEqual({ value: "ABC123", type: SERIAL_NUMBER });
    });

    it("returns empty UNSPECIFIED for empty or whitespace-only input", () => {
      expect(parseScannedIdentifier("")).toEqual({ value: "", type: UNSPECIFIED });
      expect(parseScannedIdentifier("   ")).toEqual({ value: "", type: UNSPECIFIED });
      expect(parseScannedIdentifier("SN:")).toEqual({ value: "", type: SERIAL_NUMBER });
    });
  });
});
