// @vitest-environment node
import { describe, expect, it } from "vitest";

import { parseEvidenceDocument } from "./evidence";

describe("parseEvidenceDocument", () => {
  it("reads sources, omissions and stack dropouts, tolerating malformed parts", () => {
    const document = parseEvidenceDocument(
      JSON.stringify({
        sources: { logs: { status: "ok", count: 2 }, can_status: { status: "sideways" }, health: "bad" },
        omitted: ["asic_frames", 3],
        events: { stack_dropouts: [{ stack: 1, detected_at: "2026-11-14T08:00:00Z" }, "bad"] },
      }),
    );
    expect(document).toEqual({
      sources: [
        { name: "can_status", status: "unknown" },
        { name: "health", status: "unknown" },
        { name: "logs", status: "ok", count: 2 },
      ],
      omitted: ["asic_frames"],
      stackDropouts: [{ stack: 1, detectedAt: "2026-11-14T08:00:00Z" }],
    });
  });

  it("returns undefined for a payload that is not a JSON object", () => {
    expect(parseEvidenceDocument("")).toBeUndefined();
    expect(parseEvidenceDocument("[1]")).toBeUndefined();
  });
});
