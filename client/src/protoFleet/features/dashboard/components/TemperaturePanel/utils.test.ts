// @vitest-environment node
import { describe, expect, it } from "vitest";
import { generateTemperatureHeadline } from "./utils";

const noReadings = [[{ datetime: 0, cold: 0, ok: 0, hot: 0, critical: 0 }]];

describe("generateTemperatureHeadline", () => {
  it("shows missing account readings independently of the asset label", () => {
    expect(generateTemperatureHeadline(noReadings, { audience: "account", assetLabel: "module" })).toBe(
      "No temperature readings",
    );
  });

  it("preserves the fleet headline when no temperature statuses are recorded", () => {
    expect(generateTemperatureHeadline(noReadings)).toBe("All miners within optimal range");
  });

  it("uses the supplied asset label for observed temperatures", () => {
    expect(
      generateTemperatureHeadline([[{ datetime: 0, hot: 1 }]], { audience: "account", assetLabel: "module" }),
    ).toBe("1 module outside of safe range");
  });
});
