import { describe, expect, it } from "vitest";

import {
  formatEfficiency,
  formatEfficiencyOrDash,
  formatHashrate,
  formatHashrateOrDash,
  formatPowerMwOrDash,
  formatPowerUsedCapacity,
} from "./telemetryFormat";

describe("formatHashrate", () => {
  it("returns null for missing measurements", () => {
    expect(formatHashrate(null)).toBeNull();
  });

  it("renders 0 in TH/s rather than scaling into smaller units", () => {
    expect(formatHashrate(0)).toBe("0 TH/s");
  });

  it("scales sub-TH/s values into GH/s", () => {
    // 0.4 TH/s → 400 GH/s (single test miner case)
    expect(formatHashrate(0.4)).toBe("400.0 GH/s");
  });

  it("keeps mid-range values in TH/s", () => {
    expect(formatHashrate(400)).toBe("400.0 TH/s");
  });

  it("scales thousands of TH/s into PH/s", () => {
    // 5500 TH/s → 5.50 PH/s (two decimals below 10)
    expect(formatHashrate(5_500)).toBe("5.50 PH/s");
  });

  it("scales millions of TH/s into EH/s with two decimals under 10", () => {
    expect(formatHashrate(2_500_000)).toBe("2.50 EH/s");
  });

  it("drops to one decimal above 10 EH/s and separates thousands", () => {
    expect(formatHashrate(42_000_000)).toBe("42.0 EH/s");
    expect(formatHashrate(1_234_000_000)).toBe("1,234.0 EH/s");
  });
});

describe("formatPowerUsedCapacity", () => {
  it("formats used MW and capacity MW", () => {
    // kW → MW: 12_345 kW / 1000 = 12.3 MW
    expect(formatPowerUsedCapacity(12_345, 20)).toBe("12.3 / 20.0 MW");
  });

  it("shows an em dash for the missing side when capacity is unset", () => {
    expect(formatPowerUsedCapacity(5_000, 0)).toBe("5.0 / — MW");
  });

  it("shows an em dash for the missing side when usage is unknown", () => {
    expect(formatPowerUsedCapacity(null, 20)).toBe("— / 20.0 MW");
  });

  it("returns null when both sides are missing", () => {
    expect(formatPowerUsedCapacity(null, 0)).toBeNull();
  });
});

describe("formatEfficiency", () => {
  it("returns null when efficiency is unknown", () => {
    expect(formatEfficiency(null)).toBeNull();
  });

  it("renders J/TH with one decimal", () => {
    expect(formatEfficiency(28.456)).toBe("28.5 J/TH");
  });
});

describe("OrDash variants", () => {
  it("returns em dash on null input", () => {
    expect(formatHashrateOrDash(null)).toBe("—");
    expect(formatEfficiencyOrDash(null)).toBe("—");
    expect(formatPowerMwOrDash(null)).toBe("—");
  });

  it("delegates formatting to the null-returning helpers when a value is present", () => {
    expect(formatHashrateOrDash(500)).toBe("500.0 TH/s");
    expect(formatEfficiencyOrDash(28.5)).toBe("28.5 J/TH");
    expect(formatPowerMwOrDash(12_345)).toBe("12.3 MW");
  });
});
