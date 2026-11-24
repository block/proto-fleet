import { expect, test } from "vitest";
import { observationTime, readingStatus } from "./readingFreshness";

const now = Date.parse("2026-11-24T12:00:00Z");
test.each([
  [undefined, "unavailable"],
  [NaN, "unavailable"],
  [Infinity, "unavailable"],
  [now + 5_001, "unavailable"],
  [now + 5_000, "current"],
  [now, "current"],
  [now - 119_999, "current"],
  [now - 120_000, "stale"],
] as const)("classifies observation %s as %s", (at, expected) => {
  expect(readingStatus(at, now)).toBe(expected);
});
test.each([undefined, "", "invalid", new Date(now + 6_000).toISOString()])("rejects invalid frame %s", (timestamp) => {
  expect(observationTime(timestamp, 10_000, now)).toBeUndefined();
});
test.each([-1, NaN, Infinity])("rejects invalid sample age %s", (age) => {
  expect(observationTime(new Date(now).toISOString(), age, now)).toBeUndefined();
});
test("preserves source sample age and stale observation times", () => {
  const at = observationTime(new Date(now).toISOString(), 120_000, now);
  expect(at).toBe(now - 120_000);
  expect(readingStatus(at, now)).toBe("stale");
  expect(observationTime(new Date(now - 120_000).toISOString(), 0, now)).toBe(at);
});
