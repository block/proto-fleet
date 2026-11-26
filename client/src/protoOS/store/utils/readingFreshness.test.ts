import { expect, test } from "vitest";
import { ControllerClock, readingStatus, sourceObservationTime } from "./readingFreshness";

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
test.each([undefined, "", "invalid"])("rejects invalid frame %s", (timestamp) => {
  expect(sourceObservationTime(timestamp, 10_000)).toBeUndefined();
});
test.each([-1, NaN, Infinity])("rejects invalid sample age %s", (age) => {
  expect(sourceObservationTime(new Date(now).toISOString(), age)).toBeUndefined();
});
test("stores the enclosing frame separately from the adjusted sample time", () => {
  expect(sourceObservationTime(new Date(now).toISOString(), 120_000)).toEqual({
    frameAt: now,
    observedAt: now - 120_000,
  });
  expect(sourceObservationTime(new Date(now + 6_000).toISOString(), 10_000)).toEqual({
    frameAt: now + 6_000,
    observedAt: now - 4_000,
  });
});

const evaluatedAt = new Date(now).toISOString();

test("ages readings from the request midpoint after polling stops", () => {
  const clock = new ControllerClock();
  clock.observe(evaluatedAt, clock.startObservation(100), 500);
  expect(clock.now(500)).toBe(now + 200);
  expect(readingStatus(now, clock.now(120_299))).toBe("current");
  expect(readingStatus(now, clock.now(120_300))).toBe("stale");
});

test("ignores older requests and repeated controller evaluations", () => {
  const clock = new ControllerClock();
  const older = clock.startObservation(100);
  clock.observe(evaluatedAt, clock.startObservation(100), 100);
  clock.observe(new Date(now - 60_000).toISOString(), older, 150);
  expect(clock.now(150)).toBe(now + 50);
  clock.observe(evaluatedAt, clock.startObservation(200), 200);
  expect(clock.now(200)).toBe(now + 100);
});

test("requires a valid controller timestamp from a short enough request", () => {
  const clock = new ControllerClock();
  expect(readingStatus(now, clock.now(0))).toBe("unavailable");
  clock.observe(evaluatedAt, clock.startObservation(0, now), 10_001, now + 10_001);
  expect(clock.now(10_001)).toBeNaN();
  clock.observe(evaluatedAt, clock.startObservation(11_000, now + 11_000), 21_000, now + 21_000);
  expect(clock.now(21_000)).toBe(now + 5_000);
  clock.observe("invalid", clock.startObservation(22_000, now + 22_000), 22_000, now + 22_000);
  expect(readingStatus(now, clock.now(22_000))).toBe("unavailable");
});

test("requires a new observation after a gap between browser clocks", () => {
  const clock = new ControllerClock();
  const wallMs = now - 120_000;
  clock.observe(evaluatedAt, clock.startObservation(100, wallMs), 100, wallMs);
  const pending = clock.startObservation(110, wallMs + 10);
  expect(clock.checkForDiscontinuity("timer", 110, wallMs + 180_010)).toBe(true);
  expect(clock.now(110)).toBeNaN();
  expect(clock.checkForDiscontinuity("timer", 110, wallMs + 180_010)).toBe(false);

  clock.observe(new Date(now + 5).toISOString(), pending, 110, wallMs + 180_010);
  clock.observe(evaluatedAt, clock.startObservation(110, wallMs + 180_010), 110, wallMs + 180_010);
  expect(clock.now(110)).toBeNaN();

  clock.observe(
    new Date(now + 180_020).toISOString(),
    clock.startObservation(110, wallMs + 180_010),
    130,
    wallMs + 180_030,
  );
  expect(clock.now(130)).toBe(now + 180_030);
});

test("keeps the observation when the monotonic clock advances with wall time", () => {
  const clock = new ControllerClock();
  clock.observe(evaluatedAt, clock.startObservation(100, now), 100, now);
  expect(clock.checkForDiscontinuity("timer", 110, now + 10)).toBe(false);
  expect(clock.checkForDiscontinuity("timer", 180_110, now + 180_010)).toBe(false);
  expect(clock.now(180_110)).toBe(now + 180_010);
  expect(readingStatus(now, clock.now(180_110))).toBe("stale");
});

test("a post-wake poll can confirm the clock before the timer checks it", () => {
  const clock = new ControllerClock();
  clock.observe(evaluatedAt, clock.startObservation(100, now), 100, now);
  clock.observe(new Date(now + 180_020).toISOString(), clock.startObservation(110, now + 180_010), 130, now + 180_030);
  expect(clock.checkForDiscontinuity("timer", 130, now + 180_030)).toBe(false);
  expect(clock.now(130)).toBe(now + 180_030);
});
