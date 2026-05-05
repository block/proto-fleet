import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { stripSortFromSearchParams, summarizeFilters, summarizeSort } from "./viewSummary";
import { type DeviceSet, DeviceSetSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";

const makeDeviceSet = (id: bigint, label: string): DeviceSet => create(DeviceSetSchema, { id, label });

describe("summarizeFilters", () => {
  const ctx = {
    availableGroups: [makeDeviceSet(1n, "Site A"), makeDeviceSet(2n, "Site B")],
    availableRacks: [makeDeviceSet(10n, "R1"), makeDeviceSet(11n, "R2")],
  };

  it("returns empty list when no filters are present", () => {
    expect(summarizeFilters(new URLSearchParams(""), ctx)).toEqual([]);
  });

  it("humanizes statuses", () => {
    const result = summarizeFilters(new URLSearchParams("status=offline&status=hashing"), ctx);
    expect(result).toEqual([{ key: "status", label: "Status", values: ["Hashing", "Offline"] }]);
  });

  it("humanizes issues", () => {
    const result = summarizeFilters(new URLSearchParams("issues=fans&issues=psu"), ctx);
    expect(result).toEqual([{ key: "issues", label: "Issues", values: ["Fans", "PSU"] }]);
  });

  it("preserves model and firmware values verbatim", () => {
    const result = summarizeFilters(new URLSearchParams("model=S21&model=S19&firmware=1.0.5"), ctx);
    expect(result).toContainEqual({ key: "model", label: "Model", values: ["S19", "S21"] });
    expect(result).toContainEqual({ key: "firmware", label: "Firmware", values: ["1.0.5"] });
  });

  it("looks up group and rack ids against available device sets", () => {
    const result = summarizeFilters(new URLSearchParams("group=1&group=2&rack=10"), ctx);
    expect(result).toContainEqual({ key: "group", label: "Groups", values: ["Site A", "Site B"] });
    expect(result).toContainEqual({ key: "rack", label: "Racks", values: ["R1"] });
  });

  it("falls back to an id placeholder when a group/rack is not in context", () => {
    const result = summarizeFilters(new URLSearchParams("group=999"), ctx);
    expect(result).toEqual([{ key: "group", label: "Groups", values: ["#999"] }]);
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
