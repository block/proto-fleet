import { describe, expect, it } from "vitest";

import { BUILDING_URL_PARAM, parseBuildingIdsFromParams } from "./buildingFilterUrl";

describe("parseBuildingIdsFromParams", () => {
  it("returns empty array when no building param present", () => {
    expect(parseBuildingIdsFromParams(new URLSearchParams(""))).toEqual([]);
    expect(parseBuildingIdsFromParams(new URLSearchParams("zone=Room+2"))).toEqual([]);
  });

  it("parses a single numeric id", () => {
    expect(parseBuildingIdsFromParams(new URLSearchParams("building=10"))).toEqual([10n]);
  });

  it("parses repeated keys (`?building=1&building=2`)", () => {
    expect(parseBuildingIdsFromParams(new URLSearchParams("building=1&building=2"))).toEqual([1n, 2n]);
  });

  it("parses legacy comma-joined values (`?building=1,2`)", () => {
    expect(parseBuildingIdsFromParams(new URLSearchParams("building=1,2"))).toEqual([1n, 2n]);
  });

  it("mixes repeated keys and comma-joined values", () => {
    expect(parseBuildingIdsFromParams(new URLSearchParams("building=1,2&building=3"))).toEqual([1n, 2n, 3n]);
  });

  it("trims whitespace within comma-joined values", () => {
    expect(parseBuildingIdsFromParams(new URLSearchParams("building=1%2C+2"))).toEqual([1n, 2n]);
  });

  it("skips empty entries", () => {
    expect(parseBuildingIdsFromParams(new URLSearchParams("building=&building=5"))).toEqual([5n]);
    expect(parseBuildingIdsFromParams(new URLSearchParams("building=,,7"))).toEqual([7n]);
  });

  it("rejects non-numeric values", () => {
    expect(parseBuildingIdsFromParams(new URLSearchParams("building=foo"))).toEqual([]);
    expect(parseBuildingIdsFromParams(new URLSearchParams("building=10abc"))).toEqual([]);
    expect(parseBuildingIdsFromParams(new URLSearchParams("building=-3"))).toEqual([]);
  });

  it("rejects floating-point and signed values", () => {
    expect(parseBuildingIdsFromParams(new URLSearchParams("building=1.5"))).toEqual([]);
    // URLSearchParams decodes `+` as space, so `?building=+4` parses to "4".
    // Test the literal sign case using percent-encoding (`%2B4` → `+4`).
    expect(parseBuildingIdsFromParams(new URLSearchParams("building=%2B4"))).toEqual([]);
  });

  it("keeps valid entries when other entries are invalid", () => {
    expect(parseBuildingIdsFromParams(new URLSearchParams("building=foo,42,bar"))).toEqual([42n]);
  });

  it("preserves order across repeated entries", () => {
    expect(parseBuildingIdsFromParams(new URLSearchParams("building=3&building=1&building=2"))).toEqual([3n, 1n, 2n]);
  });

  it("handles very large ids (beyond Number.MAX_SAFE_INTEGER)", () => {
    const big = "9007199254740993"; // 2^53 + 1
    expect(parseBuildingIdsFromParams(new URLSearchParams(`building=${big}`))).toEqual([BigInt(big)]);
  });

  it("exports the URL param name so callers stay in sync", () => {
    expect(BUILDING_URL_PARAM).toBe("building");
  });
});
