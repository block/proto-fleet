// @vitest-environment node
import { describe, expect, it } from "vitest";
import { getMinerCountSubtitle } from "./minerCountSubtitle";

describe("getMinerCountSubtitle", () => {
  it.each([
    { name: "some miners are not reporting", deviceCount: 3, totalMiners: 5, expected: "3 of 5 miners reporting" },
    { name: "only one miner is reporting", deviceCount: 1, totalMiners: 10, expected: "1 of 10 miners reporting" },
    { name: "zero miners are reporting", deviceCount: 0, totalMiners: 5, expected: "0 of 5 miners reporting" },
    { name: "the numbers are large", deviceCount: 999, totalMiners: 1000, expected: "999 of 1000 miners reporting" },
  ])("returns subtitle when $name", ({ deviceCount, totalMiners, expected }) => {
    expect(getMinerCountSubtitle(deviceCount, totalMiners)).toBe(expected);
  });

  it.each([
    { name: "all miners are reporting", deviceCount: 5, totalMiners: 5 },
    { name: "device count equals total miners", deviceCount: 10, totalMiners: 10 },
  ])("returns undefined when $name", ({ deviceCount, totalMiners }) => {
    expect(getMinerCountSubtitle(deviceCount, totalMiners)).toBeUndefined();
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
});
