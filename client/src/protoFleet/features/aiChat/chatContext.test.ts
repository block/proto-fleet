import { describe, expect, test } from "vitest";

import { getChatContext } from "./chatContext";

const labelsFor = (pathname: string) => getChatContext(pathname).suggestions.map((suggestion) => suggestion.label);

describe("chatContext", () => {
  test("uses dashboard prompts as the default context", () => {
    expect(labelsFor("/")).toEqual([
      "Summarize fleet health",
      "Find profit risks today",
      "What needs attention first?",
    ]);
    expect(labelsFor("/dashboard")).toEqual([
      "Summarize fleet health",
      "Find profit risks today",
      "What needs attention first?",
    ]);
  });

  test("normalizes site-scoped routes before resolving context", () => {
    expect(labelsFor("/cedar-creek/fleet/miners")).toEqual([
      "Which miners are offline?",
      "Find firmware drift",
      "Check weak or shared passwords",
    ]);
    expect(labelsFor("/north-yard/energy")).toEqual([
      "Summarize curtailment impact",
      "Find profitable power changes",
      "Plan today's power strategy",
    ]);
  });

  test("uses broader prompts for the dedicated Minerbot page", () => {
    expect(labelsFor("/minerbot")).toEqual([
      "Start a fleet health review",
      "Plan recurring work",
      "Find tasks to automate",
    ]);
  });

  test("uses more specific prompts for detail and settings routes", () => {
    expect(labelsFor("/racks/12")).toEqual([
      "Find rack hotspots",
      "Show underperforming slots",
      "Suggest miner placement fixes",
    ]);
    expect(labelsFor("/settings/agents")).toEqual([
      "Check Minerbot setup",
      "Help choose a provider",
      "What can Minerbot do here?",
    ]);
    expect(labelsFor("/settings/firmware")).toEqual([
      "Review firmware defaults",
      "Find miners behind firmware",
      "Plan a staged update",
    ]);
  });
});
