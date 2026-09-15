import { describe, expect, it } from "vitest";
import { formatMetric, getMetricValue } from "./metricDisplay";
import type { AsicData } from "@/protoOS/store";

describe("ASIC metric display", () => {
  it("normalizes units before comparing values", () => {
    expect(getMetricValue({ voltage: { latest: { value: 0.85, units: "V" } } } as AsicData, "voltage")).toBe(850);
    expect(getMetricValue({ hashrate: { latest: { value: 0.3, units: "TH/s" } } } as AsicData, "hashrate")).toBe(300);
    expect(formatMetric("temperature", 100, "F")).toBe("212.0 °F");
  });

  it("formats zero and missing readings", () => {
    expect(formatMetric("hashrate", 0, "C")).toBe("0.0 GH/s");
    expect(formatMetric("temperature", undefined, "C")).toBe("—");
  });
});
