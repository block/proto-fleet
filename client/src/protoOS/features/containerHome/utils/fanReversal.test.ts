import { describe, expect, test } from "vitest";
import {
  fanReversalAvailability,
  fanReversalDeadlineMs,
  fanReversalOutcome,
  HOSTED_FAN_REVERSAL_REASON,
} from "./fanReversal";
import { FAN_REVERSAL_OUTCOME_UNKNOWN } from "@/protoOS/api/adapters/fanReversalMessages";

const adapter = { runDryCoolerFanReversal: async () => ({ state: "finished" as const }) };
const standby = { status: { state: "finished" as const, durationMinutes: 5 }, readAtMs: 2_000 };

describe("fanReversalAvailability", () => {
  test.each([
    ["Fleet hosting", { isFleetHosted: true, readback: standby }, HOSTED_FAN_REVERSAL_REASON],
    ["no command", { adapter: {}, readback: standby }, "This connection cannot run a fan reversal."],
    ["a setting still saving", { readback: standby, settingsPending: true }, "Wait for the current setting to save."],
    [
      "a dismissed uncertain run without a newer reading",
      {
        readback: standby,
        outcome: {
          phase: "unknown" as const,
          message: FAN_REVERSAL_OUTCOME_UNKNOWN,
          settledAtMs: 2_000,
          dismissed: true,
        },
      },
      "Waiting for a new controller reading before another run.",
    ],
    ["no reading", { readback: undefined }, "Waiting for the controller to report the fan reversal state."],
    [
      "an active run",
      { readback: { status: { state: "inProgress" as const }, readAtMs: 2_000 } },
      "The controller reports a fan reversal running.",
    ],
    [
      "a starting run",
      { readback: { status: { state: "starting" as const }, readAtMs: 2_000 } },
      "The controller reports a fan reversal running.",
    ],
    [
      "an unknown state",
      { readback: { status: { state: "unknown" as const }, readAtMs: 2_000 } },
      "Unavailable until the controller reports the fan reversal on standby.",
    ],
    [
      "an uncertain run without a newer reading",
      {
        readback: standby,
        outcome: { phase: "unknown" as const, message: FAN_REVERSAL_OUTCOME_UNKNOWN, settledAtMs: 2_000 },
      },
      "Waiting for a new controller reading before another run.",
    ],
  ])("is unavailable with %s", (_name, input, reason) => {
    expect(fanReversalAvailability({ adapter, isFleetHosted: false, ...input })).toEqual({
      available: false,
      reason,
    });
  });

  test("is unavailable without a reason while this run is open", () => {
    expect(
      fanReversalAvailability({
        adapter,
        isFleetHosted: false,
        readback: standby,
        outcome: { phase: "running", durationMinutes: 5 },
      }),
    ).toEqual({ available: false });
  });

  test.each([
    ["stopped", { status: { state: "stopped" as const }, readAtMs: 1 }],
    ["finished", standby],
  ])("is available on standby (%s)", (_name, readback) => {
    expect(fanReversalAvailability({ adapter, isFleetHosted: false, readback })).toEqual({ available: true });
  });

  test("is available after an uncertain run once a newer reading reports standby", () => {
    expect(
      fanReversalAvailability({
        adapter,
        isFleetHosted: false,
        readback: standby,
        outcome: { phase: "unknown", message: FAN_REVERSAL_OUTCOME_UNKNOWN, settledAtMs: 1_999 },
      }),
    ).toEqual({ available: true });
  });
});

describe("fanReversalOutcome", () => {
  const base = { durationMinutes: 5, nowMs: 9_000 };

  test("reports a refusal with the controller's message", () => {
    const error = Object.assign(
      new Error("The controller refused the request: busy. The fan reversal did not start."),
      {
        writeOutcome: "refused" as const,
      },
    );
    expect(fanReversalOutcome({ ...base, error })).toEqual({
      phase: "failed",
      kind: "refused",
      message: error.message,
    });
  });

  test.each([{ status: 401 }, { error: { code: 16 } }])(
    "reports an authentication failure as not started: %j",
    (error) => {
      expect(fanReversalOutcome({ ...base, error })).toMatchObject({ phase: "failed", kind: "refused" });
    },
  );

  test.each([
    ["an unknown write", Object.assign(new Error("lost"), { writeOutcome: "unknown" })],
    ["a client deadline", new DOMException("timed out", "TimeoutError")],
    ["no answer", undefined],
  ])("reports %s as an unknown outcome", (_name, error) => {
    expect(fanReversalOutcome({ ...base, error })).toEqual({
      phase: "unknown",
      message: FAN_REVERSAL_OUTCOME_UNKNOWN,
      settledAtMs: 9_000,
    });
  });

  test("reports a stopped run as failed, not refused", () => {
    expect(fanReversalOutcome({ ...base, result: { state: "stopped" } })).toMatchObject({
      phase: "failed",
      kind: "stopped",
    });
  });

  test.each(["starting", "inProgress", "unknown"] as const)("treats a %s terminal answer as unknown", (state) => {
    expect(fanReversalOutcome({ ...base, result: { state } })).toMatchObject({ phase: "unknown" });
  });

  test("reports the controller's duration for a finished run", () => {
    expect(
      fanReversalOutcome({
        ...base,
        result: { state: "finished", durationMinutes: 4 },
        modeBefore: true,
        modeAfter: true,
      }),
    ).toEqual({
      phase: "completed",
      message: "The controller reports the fan reversal finished after 4 minutes.",
      tone: "neutral",
    });
  });

  test("warns when the dry cooler mode reads differently after the run", () => {
    expect(
      fanReversalOutcome({ ...base, result: { state: "finished" }, modeBefore: true, modeAfter: false }),
    ).toMatchObject({
      tone: "warning",
      message: expect.stringContaining("now reads Manual; it was Auto before the run"),
    });
  });

  test("warns when the dry cooler mode could not be re-read", () => {
    expect(
      fanReversalOutcome({ ...base, result: { state: "finished" }, modeBefore: false, modeAfter: null }),
    ).toMatchObject({ tone: "warning", message: expect.stringContaining("check it is still Manual") });
  });
});

test("waits past Frigate's activation and completion windows", () => {
  expect(fanReversalDeadlineMs(3)).toBe(15_000 + 5 * 60_000 + 30_000);
  expect(fanReversalDeadlineMs(10)).toBe(15_000 + 12 * 60_000 + 30_000);
});
