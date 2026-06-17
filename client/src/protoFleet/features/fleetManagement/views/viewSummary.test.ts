import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  diffDisplaySummaries,
  stripDisplayFromSearchParams,
  stripSortFromSearchParams,
  summarizeDisplay,
  summarizeFilters,
  summarizeSort,
} from "./viewSummary";
import { type DeviceSet, DeviceSetSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";

const makeDeviceSet = (id: bigint, label: string): DeviceSet => create(DeviceSetSchema, { id, label });

describe("summarizeFilters", () => {
  const ctx = {
    availableGroups: [makeDeviceSet(1n, "Site A"), makeDeviceSet(2n, "Site B")],
    availableRacks: [makeDeviceSet(10n, "R1"), makeDeviceSet(11n, "R2")],
    availableBuildings: [
      { id: "100", label: "DC1" },
      { id: "101", label: "DC2" },
    ],
    availableSites: [
      { id: "200", label: "Houston" },
      { id: "201", label: "Austin" },
    ],
  };

  it("returns empty list when no filters are present", () => {
    expect(summarizeFilters(new URLSearchParams(""), "miners", ctx)).toEqual([]);
  });

  it("humanizes statuses on the miners tab", () => {
    const result = summarizeFilters(new URLSearchParams("status=offline&status=hashing"), "miners", ctx);
    expect(result).toEqual([{ key: "status", label: "Status", values: ["Hashing", "Offline"] }]);
  });

  it("humanizes issues on the miners tab", () => {
    const result = summarizeFilters(new URLSearchParams("issues=fans&issues=psu"), "miners", ctx);
    expect(result).toEqual([{ key: "issues", label: "Issues", values: ["Fans", "PSU"] }]);
  });

  it("preserves model and firmware values verbatim", () => {
    const result = summarizeFilters(new URLSearchParams("model=S21&model=S19&firmware=1.0.5"), "miners", ctx);
    expect(result).toContainEqual({ key: "model", label: "Model", values: ["S19", "S21"] });
    expect(result).toContainEqual({ key: "firmware", label: "Firmware", values: ["1.0.5"] });
  });

  it("looks up group and rack ids against available device sets", () => {
    const result = summarizeFilters(new URLSearchParams("group=1&group=2&rack=10"), "miners", ctx);
    expect(result).toContainEqual({ key: "group", label: "Groups", values: ["Site A", "Site B"] });
    expect(result).toContainEqual({ key: "rack", label: "Racks", values: ["R1"] });
  });

  it("falls back to an id placeholder when a group/rack is not in context", () => {
    const result = summarizeFilters(new URLSearchParams("group=999"), "miners", ctx);
    expect(result).toEqual([{ key: "group", label: "Groups", values: ["#999"] }]);
  });

  it("renders building filter labels on the racks tab", () => {
    const result = summarizeFilters(new URLSearchParams("building=100&building=101"), "racks", ctx);
    expect(result).toContainEqual({ key: "building", label: "Buildings", values: ["DC1", "DC2"] });
  });

  it("renders site filter labels on the racks tab", () => {
    const result = summarizeFilters(new URLSearchParams("site=200"), "racks", ctx);
    expect(result).toContainEqual({ key: "site", label: "Sites", values: ["Houston"] });
  });

  it("ignores miner-only filter keys on the racks tab", () => {
    expect(summarizeFilters(new URLSearchParams("status=offline&model=S21"), "racks", ctx)).toEqual([]);
  });

  it("returns empty for tabs without a filter surface (buildings, sites)", () => {
    expect(summarizeFilters(new URLSearchParams("anything=goes"), "buildings", ctx)).toEqual([]);
    expect(summarizeFilters(new URLSearchParams("anything=goes"), "sites", ctx)).toEqual([]);
  });
});

describe("summarizeSort", () => {
  it("returns undefined when no sort param is set", () => {
    expect(summarizeSort(new URLSearchParams(""))).toBeUndefined();
  });

  it("humanizes the field name and defaults direction to desc when missing", () => {
    expect(summarizeSort(new URLSearchParams("sort=hashrate"))).toEqual({ fieldLabel: "Hashrate", direction: "desc" });
  });

  it("respects asc direction when present", () => {
    expect(summarizeSort(new URLSearchParams("sort=name&dir=asc"))).toEqual({ fieldLabel: "Name", direction: "asc" });
  });
});

describe("stripSortFromSearchParams", () => {
  it("removes sort and dir keys, leaving the rest intact", () => {
    expect(stripSortFromSearchParams("model=S21&sort=hashrate&dir=desc&status=offline")).toBe(
      "model=S21&status=offline",
    );
  });

  it("is a no-op when sort params are absent", () => {
    expect(stripSortFromSearchParams("model=S21")).toBe("model=S21");
  });
});

describe("summarizeDisplay", () => {
  it("returns undefined when no display param is set", () => {
    expect(summarizeDisplay(new URLSearchParams(""))).toBeUndefined();
  });

  it("humanizes the grid mode", () => {
    expect(summarizeDisplay(new URLSearchParams("display=grid"))).toEqual({ mode: "grid", label: "Grid view" });
  });

  it("humanizes the list mode", () => {
    expect(summarizeDisplay(new URLSearchParams("display=list"))).toEqual({ mode: "list", label: "List view" });
  });

  it("ignores unknown display values", () => {
    expect(summarizeDisplay(new URLSearchParams("display=carousel"))).toBeUndefined();
  });
});

describe("diffDisplaySummaries", () => {
  const grid = { mode: "grid" as const, label: "Grid view" };
  const list = { mode: "list" as const, label: "List view" };

  it("marks unchanged when current and saved match", () => {
    expect(diffDisplaySummaries(grid, grid)).toEqual({ change: "unchanged", current: grid, saved: grid });
  });

  it("marks added when only current is present", () => {
    expect(diffDisplaySummaries(grid, undefined)).toEqual({ change: "added", current: grid, saved: undefined });
  });

  it("marks removed when only saved is present", () => {
    expect(diffDisplaySummaries(undefined, list)).toEqual({ change: "removed", current: undefined, saved: list });
  });

  it("marks changed when current differs from saved", () => {
    expect(diffDisplaySummaries(grid, list)).toEqual({ change: "changed", current: grid, saved: list });
  });

  it("is undefined when both sides are absent", () => {
    expect(diffDisplaySummaries(undefined, undefined)).toEqual({
      change: "unchanged",
      current: undefined,
      saved: undefined,
    });
  });
});

describe("stripDisplayFromSearchParams", () => {
  it("removes the display key, leaving the rest intact", () => {
    expect(stripDisplayFromSearchParams("building=100&display=grid&zone=A")).toBe("building=100&zone=A");
  });

  it("is a no-op when display is absent", () => {
    expect(stripDisplayFromSearchParams("building=100")).toBe("building=100");
  });
});
