import { describe, expect, test } from "vitest";
import { voltageOutcome } from "./moduleVoltageView";
import { MODULE_VOLTAGE_OUTCOME_UNKNOWN, type ModuleVoltageSetting } from "@/protoOS/api/adapters/moduleVoltage";

const setting = (fixedMillivolts?: number): ModuleVoltageSetting => ({
  moduleId: "0011223344556677",
  boardCount: 3,
  readiness: "ready",
  ...(fixedMillivolts === undefined ? {} : { fixedMillivolts }),
});
const saved = { result: { boardsUpdated: 3, boards: [] } };
const unknown = { error: { message: MODULE_VOLTAGE_OUTCOME_UNKNOWN, writeOutcome: "unknown" as const } };
const refused = { error: { message: "The controller refused the request.", writeOutcome: "refused" as const } };
const MISSING = "The controller no longer lists this module. It may have been replaced.";

describe("voltageOutcome", () => {
  test.each([
    {
      name: "unknown write, re-read failed",
      write: unknown,
      reread: undefined,
      tone: "critical",
      message: `${MODULE_VOLTAGE_OUTCOME_UNKNOWN} The current setting could not be re-read.`,
    },
    {
      name: "unknown write, module no longer listed",
      write: unknown,
      reread: null,
      tone: "critical",
      message: `${MODULE_VOLTAGE_OUTCOME_UNKNOWN} ${MISSING}`,
    },
    {
      name: "unknown write, re-read matches",
      write: unknown,
      reread: setting(28_000),
      tone: "warning",
      message:
        "The controller did not answer, but it now reports a fixed voltage of 28,000 mV. Per-hashboard results are unavailable.",
    },
    {
      name: "unknown write, re-read differs",
      write: unknown,
      reread: setting(),
      tone: "critical",
      message: `${MODULE_VOLTAGE_OUTCOME_UNKNOWN} The current setting is shown above.`,
    },
    { name: "saved write, module no longer listed", write: saved, reread: null, tone: "critical", message: MISSING },
    {
      name: "saved write, re-read failed",
      write: saved,
      reread: undefined,
      tone: "warning",
      message:
        "The controller saved the change, but the current setting could not be re-read. Reload before changing it again.",
    },
    {
      name: "refused write, setting unchanged",
      write: refused,
      reread: setting(),
      tone: "critical",
      message: refused.error.message,
    },
    {
      name: "refused write, re-read failed",
      write: refused,
      reread: undefined,
      tone: "critical",
      message: refused.error.message,
    },
    {
      name: "refused write, setting changed anyway",
      write: refused,
      reread: setting(28_000),
      tone: "critical",
      message:
        "The controller refused the request, but it now reports a fixed voltage of 28,000 mV. Check the setting before trying again.",
    },
  ])("$name", ({ write, reread, tone, message }) => {
    expect(voltageOutcome({ request: 28_000, write, previous: setting(), reread, boardLabel: (mac) => mac })).toEqual({
      tone,
      message,
    });
  });
});
