import { describe, expect, test } from "vitest";

import { observationState, observationSummary } from "./controllerObservation";

const now = Date.parse("2026-11-13T10:00:00Z");

describe("observationState", () => {
  test.each([
    ["missing", undefined],
    ["unknown", { freshness: "unspecified" as const, observedAtMs: now }],
    ["unknown", { freshness: "fresh" as const }],
    ["unknown", { freshness: "fresh" as const, observedAtMs: now + 60_000 }],
    ["stale", { freshness: "stale" as const, observedAtMs: now }],
    // A sample reported fresh but received late has still aged out.
    ["stale", { freshness: "fresh" as const, observedAtMs: now - 16_000 }],
    ["fresh", { freshness: "fresh" as const, observedAtMs: now - 3_000 }],
  ])("%s for %j", (expected, observation) => {
    expect(observationState(observation, now)).toBe(expected);
  });
});

describe("observationSummary", () => {
  test("states age and the last success for an unhealthy observation", () => {
    const observation = { freshness: "stale" as const, observedAtMs: now - 2_000, lastSuccessfulAtMs: now - 240_000 };
    expect(observationSummary(observation, "stale", now)).toBe("Stale · observed 2s ago · last success 4m ago");
  });

  test("names missing and unknown observations without an age", () => {
    expect(observationSummary(undefined, "missing", now)).toBe("Not observed");
    expect(observationSummary({ freshness: "unspecified" }, "unknown", now)).toBe("Freshness unknown");
  });
});
