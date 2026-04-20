import { describe, expect, it } from "vitest";
import { normalizeEfficiencyToJTH, normalizeHashrateToTHs, normalizePowerToKW } from "./metricNormalization";

describe("metricNormalization", () => {
  describe("normalizeEfficiencyToJTH", () => {
    it("keeps already-normalized J/TH values unchanged", () => {
      expect(normalizeEfficiencyToJTH(24.4)).toBe(24.4);
    });

    it("converts raw J/H values to J/TH", () => {
      expect(normalizeEfficiencyToJTH(24.4e-12)).toBeCloseTo(24.4);
    });

    it("converts accidentally over-converted efficiency values back to J/TH", () => {
      expect(normalizeEfficiencyToJTH(24.4e12)).toBeCloseTo(24.4);
    });
  });

  describe("normalizePowerToKW", () => {
    it("keeps already-normalized kW values unchanged", () => {
      expect(normalizePowerToKW(3.6, 1)).toBe(3.6);
    });

    it("converts raw W values to kW", () => {
      expect(normalizePowerToKW(3600, 1)).toBe(3.6);
    });

    it("keeps low valid kW values unchanged", () => {
      expect(normalizePowerToKW(0.0036, 1)).toBe(0.0036);
    });

    it("skips normalization when deviceCount is invalid", () => {
      expect(normalizePowerToKW(3600, 0)).toBe(3600);
      expect(normalizePowerToKW(3600, NaN)).toBe(3600);
    });
  });

  describe("normalizeHashrateToTHs", () => {
    it("keeps already-normalized TH/s values unchanged", () => {
      expect(normalizeHashrateToTHs(120, 1)).toBe(120);
    });

    it("converts raw H/s values to TH/s", () => {
      expect(normalizeHashrateToTHs(120e12, 1)).toBe(120);
    });

    it("keeps low valid TH/s values unchanged", () => {
      expect(normalizeHashrateToTHs(120e-12, 1)).toBe(120e-12);
    });

    it("skips normalization when deviceCount is invalid", () => {
      expect(normalizeHashrateToTHs(120e12, 0)).toBe(120e12);
      expect(normalizeHashrateToTHs(120e12, NaN)).toBe(120e12);
    });
  });
});
