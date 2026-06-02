import { describe, expect, it } from "vitest";

import { inferTimezone } from "./inferTimezone";

describe("inferTimezone", () => {
  it("returns the IANA zone for known US states", () => {
    expect(inferTimezone("US", "CA")).toBe("America/Los_Angeles");
    expect(inferTimezone("US", "NY")).toBe("America/New_York");
    expect(inferTimezone("US", "TX")).toBe("America/Chicago");
    expect(inferTimezone("US", "AZ")).toBe("America/Phoenix");
    expect(inferTimezone("US", "HI")).toBe("Pacific/Honolulu");
  });

  it("returns the IANA zone for known CA provinces", () => {
    expect(inferTimezone("CA", "ON")).toBe("America/Toronto");
    expect(inferTimezone("CA", "BC")).toBe("America/Vancouver");
    expect(inferTimezone("CA", "AB")).toBe("America/Edmonton");
    expect(inferTimezone("CA", "SK")).toBe("America/Regina");
    expect(inferTimezone("CA", "NL")).toBe("America/St_Johns");
  });

  it("normalizes case and whitespace", () => {
    expect(inferTimezone("us", "ca")).toBe("America/Los_Angeles");
    expect(inferTimezone("  US ", " cA ")).toBe("America/Los_Angeles");
    expect(inferTimezone("ca", "on")).toBe("America/Toronto");
  });

  it("defaults empty country to US", () => {
    expect(inferTimezone("", "CA")).toBe("America/Los_Angeles");
    expect(inferTimezone("   ", "NY")).toBe("America/New_York");
  });

  it("returns empty for unsupported countries", () => {
    expect(inferTimezone("MX", "DF")).toBe("");
    expect(inferTimezone("GB", "")).toBe("");
  });

  it("returns empty for unknown state codes", () => {
    expect(inferTimezone("US", "ZZ")).toBe("");
    expect(inferTimezone("US", "")).toBe("");
    expect(inferTimezone("CA", "ZZ")).toBe("");
  });
});
