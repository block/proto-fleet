import { describe, expect, it } from "vitest";

import {
  formatCurtailmentKw,
  mapCurtailmentPriority,
  mapEditableCurtailmentPriority,
  toApiCurtailmentPriority,
} from "@/protoFleet/api/curtailmentUtils";
import { CurtailmentPriority } from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";

describe("curtailment power formatting", () => {
  it("preserves fixed precision and units for operational displays", () => {
    expect(formatCurtailmentKw(12)).toBe("12.0 kW");
    expect(formatCurtailmentKw(12.34, 2)).toBe("12.34 kW");
    expect(formatCurtailmentKw(12, { includeUnit: false })).toBe("12.0");
    expect(formatCurtailmentKw(Number.NaN)).toBe("0.0 kW");
  });

  it("preserves flexible response-profile precision", () => {
    const profileFormat = { maximumFractionDigits: 2, minimumFractionDigits: 0, normalizeNonFinite: false };
    expect(formatCurtailmentKw(12, profileFormat)).toBe("12 kW");
    expect(formatCurtailmentKw(12.345, profileFormat)).toBe("12.35 kW");
    expect(formatCurtailmentKw(Number.POSITIVE_INFINITY, profileFormat)).toBe("∞ kW");
  });

  it("preserves the settings summary's default locale precision", () => {
    expect(
      formatCurtailmentKw(12.3456, {
        maximumFractionDigits: 3,
        minimumFractionDigits: 0,
        includeUnit: false,
        normalizeNonFinite: false,
      }),
    ).toBe("12.346");
  });
});

describe("curtailment priorities", () => {
  it.each([
    [CurtailmentPriority.NORMAL, "normal", "normal"],
    [CurtailmentPriority.HIGH, "high", "normal"],
    [CurtailmentPriority.EMERGENCY, "emergency", "emergency"],
    [CurtailmentPriority.UNSPECIFIED, "normal", "normal"],
  ] as const)("preserves display and editable mappings for %s", (priority, display, editable) => {
    expect(mapCurtailmentPriority(priority)).toBe(display);
    expect(mapEditableCurtailmentPriority(priority)).toBe(editable);
    expect(toApiCurtailmentPriority(editable)).toBe(
      editable === "emergency" ? CurtailmentPriority.EMERGENCY : CurtailmentPriority.NORMAL,
    );
  });
});
