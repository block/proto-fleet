import { describe, expect, it } from "vitest";
import { assessMapping } from "./feedMapping";
import type { V2GetContainerMapResponse, V2GetFeedMappingReportResponse, V2Slot } from "./generated/generatedApi";

const board = (mac = "aa:bb:cc:00:00:01"): V2Slot => ({
  occupied: true,
  hashboard: { macAddress: mac, moduleId: "cu1", location: { powerFeedId: "pdu-1-outlet-01" } },
});
const map = (tank = 1, module = 1, slots = [board()]): V2GetContainerMapResponse => ({
  containerMap: { tanks: [{ id: tank, modules: [{ id: module, slots }] }] },
});
const report: V2GetFeedMappingReportResponse = { wiring: { mode: "manual" } };

describe("mapping assessment", () => {
  it("includes and deduplicates unlocated inventory", () => {
    const result = assessMapping(map(0, 0, [board(), board(), board("aa:bb:cc:00:00:02")]), report);
    expect(result.boards).toHaveLength(2);
    expect(result.boards.every((b) => b.unlocated)).toBe(true);
  });

  it("does not classify empty slots or unverified boards as mismatches", () => {
    const result = assessMapping(map(1, 1, [board(), { occupied: false }]), report);
    expect(result.boards).toHaveLength(1);
    expect(result.boards[0]).toMatchObject({ unlocated: false, conflict: false });
  });

  it("keeps expected and observed feeds and excludes superseded evidence", () => {
    const result = assessMapping(map(), {
      wiring: {
        mode: "switch_ports",
        boards: [
          {
            macAddress: "aa:bb:cc:00:00:01",
            status: "mismatch",
            expectedFeedId: "pdu-1-outlet-01",
            observedFeedId: "pdu-1-outlet-02",
          },
          { macAddress: "old", status: "mismatch", supersededBy: "aa:bb:cc:00:00:01" },
          { status: "unverified", reasons: ["no_live_board_observed"] },
        ],
      },
    });
    expect(result.boards).toHaveLength(1);
    expect(result.boards[0]).toMatchObject({
      conflict: true,
      expectedFeed: "pdu-1-outlet-01",
      observedFeed: "pdu-1-outlet-02",
    });
  });

  it("detects one CU1 identity claimed by different physical modules", () => {
    const topology = map();
    topology.containerMap!.tanks![0].modules!.push({ id: 2, slots: [board("other")] });
    expect(assessMapping(topology, report).boards.every((b) => b.conflict)).toBe(true);
  });

  it("preserves a missing-location finding when wiring evidence is unavailable", () => {
    const result = assessMapping(map(0, 0), undefined, "Report unavailable");
    expect(result.boards[0].unlocated).toBe(true);
    expect(result.unavailable).toBe("Report unavailable");
    expect(result.mode).toBe("unknown");
  });

  it("does not treat missing topology as a healthy empty container", () => {
    expect(assessMapping({}, report).unavailable).toBeTruthy();
  });

  it("distinguishes stale evidence from a confirmed conflict", () => {
    const result = assessMapping(map(), {
      wiring: {
        mode: "switch_ports",
        boards: [{ macAddress: "aa:bb:cc:00:00:01", status: "stale", reasons: ["switch_unavailable"] }],
      },
    });
    expect(result.boards[0]).toMatchObject({ status: "stale", conflict: false });
  });

  it("does not turn an assigned feed into measured wiring evidence", () => {
    const result = assessMapping(map(), {
      wiring: {
        mode: "switch_ports",
        boards: [
          {
            macAddress: "aa:bb:cc:00:00:01",
            status: "unverified",
            expectedFeedId: "pdu-1-outlet-01",
          },
        ],
      },
    });
    expect(result.boards[0].assignedFeed).toBe("pdu-1-outlet-01");
    expect(result.boards[0].observedFeed).toBeUndefined();
  });
});
