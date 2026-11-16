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

  it("adds a Truncated column only when an event was shortened", () => {
    const csv = auditEventsToCsv([
      { id: "1", actor: "a", source: "s", node: "", action: "x", target: "t", result: "ok", truncated: true },
      { id: "2", actor: "b", source: "s", node: "", action: "y", target: "t", result: "ok" },
    ]);
    expect(csv[0]).toMatch(/,Truncated$/);
    expect(csv[1]).toMatch(/,"yes"$/);
    expect(csv[2]).toMatch(/,""$/);
  });

  it("neutralises client-supplied values a spreadsheet would evaluate", () => {
    const [, row] = auditEventsToCsv([
      { id: "1", actor: '=HYPERLINK("x")', source: "@sum", node: "", action: "+1", target: "-1", result: "ok" },
    ]);
    expect(row).toBe('"","\'=HYPERLINK(""x"")","\'@sum","","\'+1","\'-1","ok","",""');
  });

  it("leaves out the Truncated column when nothing was shortened", () => {
    expect(controllerLogsToCsv([{ severity: "info", component: "pool", message: "ok" }])).toEqual([
      "Time,Severity,Component,Message",
      '"","info","pool","ok"',
    ]);
  });
});

describe("controllerLogsToCsv", () => {
  it("keeps source timestamps as recorded and neutralises formulas", () => {
    expect(
      controllerLogsToCsv([
        { timestamp: "2026-11-13T10:00:00.123+02:00", severity: "warn", component: "pool", message: "stale job" },
        { severity: "error", component: "", message: "=1+1", truncated: true },
      ]),
    ).toEqual([
      "Time,Severity,Component,Message,Truncated",
      '"2026-11-13T10:00:00.123+02:00","warn","pool","stale job",""',
      `"","error","","'=1+1","yes"`,
    ]);
  });

  it("leaves out the Truncated column when nothing was shortened", () => {
    expect(controllerLogsToCsv([{ severity: "info", component: "pool", message: "ok" }])).toEqual([
      "Time,Severity,Component,Message",
      '"","info","pool","ok"',
    ]);
  });
});
