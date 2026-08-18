import { describe, expect, it } from "vitest";

import { inProgressFirmwareEvent } from "./rollout.fixtures";
import {
  rolloutCompletionPercent,
  rolloutErrorImpactCount,
  rolloutLifecycleActions,
  rolloutMetricDeltaIntent,
  rolloutStageLabel,
} from "./rolloutDisplayUtils";

describe("rolloutMetricDeltaIntent", () => {
  it("treats lower hashrate as a positive curtailment dispatch outcome", () => {
    expect(rolloutMetricDeltaIntent("hashrate", -1, "curtailment", "dispatch")).toBe("positive");
  });

  it("treats lower hashrate as a negative restoration outcome", () => {
    expect(rolloutMetricDeltaIntent("hashrate", -1, "curtailment", "restore")).toBe("negative");
  });

  it("treats higher temperature as negative across rollout processes", () => {
    expect(rolloutMetricDeltaIntent("temperature", 1, "firmware")).toBe("negative");
    expect(rolloutMetricDeltaIntent("temperature", 1, "curtailment", "restore")).toBe("negative");
  });
});

describe("rolloutErrorImpactCount", () => {
  it("counts each impacted miner once across multiple error strings", () => {
    expect(
      rolloutErrorImpactCount([
        { id: "one", message: "First error", impactedMiners: ["miner-1", "miner-2"] },
        { id: "two", message: "Second error", impactedMiners: ["miner-1"] },
      ]),
    ).toBe(2);
  });
});

describe("rolloutLifecycleActions", () => {
  it("keeps abort and revert as separate lifecycle actions", () => {
    const onAbort = () => undefined;
    const onRevert = () => undefined;

    expect(
      rolloutLifecycleActions({ ...inProgressFirmwareEvent, state: "running" }, { onAbort, onRevert }).map(
        (action) => action.key,
      ),
    ).toEqual(["abort"]);
    expect(
      rolloutLifecycleActions({ ...inProgressFirmwareEvent, state: "aborted" }, { onAbort, onRevert }).map(
        (action) => action.key,
      ),
    ).toEqual(["revert"]);
  });

  it("hides control actions without rollout control permission", () => {
    expect(
      rolloutLifecycleActions(
        { ...inProgressFirmwareEvent, state: "running" },
        { onAbort: () => undefined, onPause: () => undefined },
        { canControl: false },
      ),
    ).toEqual([]);
  });

  it("preserves fixture-state fallbacks and API eligibility overrides", () => {
    const handlers = {
      onContinueFromReview: () => undefined,
      onPause: () => undefined,
      onAbort: () => undefined,
    };

    expect(
      rolloutLifecycleActions({ ...inProgressFirmwareEvent, state: "pausedAtPilotGate" }, handlers).map(
        (action) => action.key,
      ),
    ).toEqual(["continue", "abort"]);
    expect(rolloutLifecycleActions(inProgressFirmwareEvent, handlers).map((action) => action.key)).toEqual([
      "pause",
      "abort",
    ]);
    expect(
      rolloutLifecycleActions(
        {
          ...inProgressFirmwareEvent,
          state: "running",
          availableActions: {
            admit: false,
            continue: false,
            pause: false,
            resume: false,
            abort: false,
            revert: false,
            complete: false,
          },
        },
        handlers,
      ),
    ).toEqual([]);
  });

  it("does not offer ordinary retry for attention-required members", () => {
    const actions = rolloutLifecycleActions(
      {
        ...inProgressFirmwareEvent,
        state: "completedWithFailures",
        rollups: [
          { phase: "failed", count: 1 },
          { phase: "attentionRequired", count: 1 },
        ],
      },
      { onRetryFailed: () => undefined },
    );

    expect(actions.map((action) => action.key)).not.toContain("retry");
  });
});

describe("rolloutStageLabel", () => {
  it.each([
    ["created", "Created"],
    ["running", "In progress"],
    ["paused", "Paused"],
    ["review", "Review"],
    ["aborted", "Aborted"],
    ["completed", "Completed"],
    ["completedWithFailures", "Completed with failures"],
    ["reverting", "Reverting"],
    ["reverted", "Reverted"],
  ] as const)("renders the API lifecycle state %s", (state, expected) => {
    expect(rolloutStageLabel({ ...inProgressFirmwareEvent, state })).toBe(expected);
  });

  it("uses reverted members for reverse-transition completion", () => {
    expect(
      rolloutCompletionPercent({
        ...inProgressFirmwareEvent,
        state: "reverted",
        totalTargets: 10,
        excludedTargets: 0,
        rollups: [
          { phase: "done", count: 10 },
          { phase: "reverted", count: 10 },
        ],
      }),
    ).toBe(100);
  });
});
