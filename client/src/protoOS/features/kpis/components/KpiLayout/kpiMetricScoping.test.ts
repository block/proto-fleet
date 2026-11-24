import { describe, expect, test } from "vitest";
import { metricFromRouteParam, perBoardFieldsForMetric } from "./kpiMetricScoping";

describe("metricFromRouteParam", () => {
  test.each([
    ["power-usage", "power"],
    ["efficiency", "efficiency"],
    ["temperature", "temperature"],
    ["hashrate", "hashrate"],
  ] as const)("%s -> %s", (param, expected) => {
    expect(metricFromRouteParam(param)).toBe(expected);
  });

  // Home's index route has no `:metric` segment, and unknown segments (e.g. a
  // deep-link typo) must not silently scope the fan-out to the wrong chart.
  test("defaults to hashrate for the index route and unknown segments", () => {
    expect(metricFromRouteParam(undefined)).toBe("hashrate");
    expect(metricFromRouteParam("bogus")).toBe("hashrate");
  });
});

describe("perBoardFieldsForMetric", () => {
  test("scopes each metric to just the fields its chart needs", () => {
    expect(perBoardFieldsForMetric("hashrate")).toEqual(["hashrate"]);
    expect(perBoardFieldsForMetric("power")).toEqual(["power"]);
    expect(perBoardFieldsForMetric("temperature")).toEqual(["temperature"]);
  });

  // Efficiency requests the "efficiency" field itself — the per-board chart
  // segments read telemetry.efficiency directly (served natively by the rig
  // adapter, derived by the CT1 adapter). Requesting raw hashrate+power here
  // would leave telemetry.efficiency empty and blank the per-board series.
  test("efficiency fetches the efficiency field", () => {
    expect(perBoardFieldsForMetric("efficiency")).toEqual(["efficiency"]);
  });
});
