import { describe, expect, it } from "vitest";
import { getMinerCountSubtitle } from "./minerCountSubtitle";

describe("getMinerCountSubtitle", () => {
  it("returns subtitle when some miners are not reporting", () => {
    const result = getMinerCountSubtitle(3, 5);
    expect(result).toBe("3 of 5 miners reporting");
  });

  it("returns subtitle when only one miner is reporting", () => {
    const result = getMinerCountSubtitle(1, 10);
    expect(result).toBe("1 of 10 miners reporting");
  });

  it("returns undefined when all miners are reporting", () => {
    const result = getMinerCountSubtitle(5, 5);
    expect(result).toBeUndefined();
  });

  it("returns undefined when device count equals total miners", () => {
    const result = getMinerCountSubtitle(10, 10);
    expect(result).toBeUndefined();
  });

  it("returns undefined when device count is greater than total miners", () => {
    const result = getMinerCountSubtitle(15, 10);
    expect(result).toBeUndefined();
  });

  it("returns undefined when device count is null", () => {
    const result = getMinerCountSubtitle(null, 5);
    expect(result).toBeUndefined();
  });

  it("returns undefined when total miners is zero", () => {
    const result = getMinerCountSubtitle(0, 0);
    expect(result).toBeUndefined();
  });

  it("returns undefined when total miners is negative", () => {
    const result = getMinerCountSubtitle(5, -1);
    expect(result).toBeUndefined();
  });

  it("returns subtitle when zero miners are reporting", () => {
    const result = getMinerCountSubtitle(0, 5);
    expect(result).toBe("0 of 5 miners reporting");
  });

  it("handles large numbers correctly", () => {
    const result = getMinerCountSubtitle(999, 1000);
    expect(result).toBe("999 of 1000 miners reporting");
  });
});
