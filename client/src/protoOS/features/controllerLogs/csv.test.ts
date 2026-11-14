import { describe, expect, it } from "vitest";

import { auditEventsToCsv, controllerLogsToCsv } from "./csv";

describe("auditEventsToCsv", () => {
  it("keeps controller identity and attribution, quoting every cell", () => {
    expect(
      auditEventsToCsv([
        {
          id: "1",
          time: "2026-11-13T10:00:00Z",
          actor: "operator@bridge",
          source: "rest:10.0.0.2:443",
          node: "ct1-a",
          action: "SetBreaker",
          target: "breaker-1",
          result: "error",
          error: 'refused: "interlock"',
          details: '{"state":"open"}',
        },
      ]),
    ).toEqual([
      "Time,Actor,Source,Controller,Action,Target,Result,Error,Details",
      '"2026-11-13T10:00:00Z","operator@bridge","rest:10.0.0.2:443","ct1-a","SetBreaker","breaker-1","error","refused: ""interlock""","{""state"":""open""}"',
    ]);
  });

  it("neutralises client-supplied values a spreadsheet would evaluate", () => {
    const [, row] = auditEventsToCsv([
      { id: "1", actor: '=HYPERLINK("x")', source: "@sum", node: "", action: "+1", target: "-1", result: "ok" },
    ]);
    expect(row).toBe('"","\'=HYPERLINK(""x"")","\'@sum","","\'+1","\'-1","ok","",""');
  });
});

describe("controllerLogsToCsv", () => {
  it("keeps source timestamps as recorded and neutralises formulas", () => {
    expect(
      controllerLogsToCsv([
        { timestamp: "2026-11-13T10:00:00.123+02:00", severity: "warn", component: "pool", message: "stale job" },
        { severity: "error", component: "", message: "=1+1" },
      ]),
    ).toEqual([
      "Time,Severity,Component,Message",
      '"2026-11-13T10:00:00.123+02:00","warn","pool","stale job"',
      `"","error","","'=1+1"`,
    ]);
  });
});
