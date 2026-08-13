import { describe, expect, it } from "vitest";
import { getRadialGaugeGeometry } from "./radialGaugeGeometry";

const base = { size: 112, strokeWidth: 8, sweep: 360 };

describe("getRadialGaugeGeometry", () => {
  it("computes radius and circumference from size and stroke", () => {
    const g = getRadialGaugeGeometry({ ...base, value: 50 });
    expect(g.radius).toBe((112 - 8) / 2);
    expect(g.circumference).toBeCloseTo(2 * Math.PI * 52);
  });

  it("fills the full circumference for a 360° track", () => {
    const g = getRadialGaugeGeometry({ ...base, value: 100 });
    expect(g.trackLength).toBeCloseTo(g.circumference);
    expect(g.trackGap).toBeCloseTo(0);
    expect(g.valueLength).toBeCloseTo(g.circumference);
  });

  it("value arc is the given fraction of the track", () => {
    const g = getRadialGaugeGeometry({ ...base, value: 45 });
    expect(g.valueLength).toBeCloseTo(0.45 * g.trackLength);
    expect(g.valueGap).toBeCloseTo(g.circumference - g.valueLength);
  });

  it("track spans only the sweep fraction of the circumference", () => {
    const g = getRadialGaugeGeometry({ ...base, sweep: 270, value: 100 });
    expect(g.trackLength).toBeCloseTo(0.75 * g.circumference);
    // A full-value arc on a partial sweep fills the whole track, not the ring.
    expect(g.valueLength).toBeCloseTo(g.trackLength);
  });

  it("clamps value below 0 and above 100", () => {
    expect(getRadialGaugeGeometry({ ...base, value: -20 }).valueLength).toBe(0);
    const over = getRadialGaugeGeometry({ ...base, value: 250 });
    expect(over.valueLength).toBeCloseTo(over.trackLength);
  });

  it("clamps sweep into 1–360", () => {
    expect(getRadialGaugeGeometry({ ...base, sweep: 0, value: 50 }).trackLength).toBeGreaterThan(0);
    const big = getRadialGaugeGeometry({ ...base, sweep: 720, value: 100 });
    expect(big.trackLength).toBeCloseTo(big.circumference);
  });

  it("clamps the radius and dash geometry when the stroke exceeds the size", () => {
    const g = getRadialGaugeGeometry({ size: 20, strokeWidth: 24, sweep: 360, value: 50 });

    expect(g.radius).toBe(0);
    expect(g.circumference).toBe(0);
    expect(g.trackLength).toBe(0);
    expect(g.trackGap).toBe(0);
    expect(g.valueLength).toBe(0);
    expect(g.valueGap).toBe(0);
  });

  it("centres a full ring at the top and rotates a partial sweep to open at the bottom", () => {
    expect(getRadialGaugeGeometry({ ...base, sweep: 360, value: 50 }).rotation).toBe(-90);
    // 270° sweep leaves a 90° gap, centred at the bottom → offset by 45°.
    expect(getRadialGaugeGeometry({ ...base, sweep: 270, value: 50 }).rotation).toBe(-135);
  });
});
