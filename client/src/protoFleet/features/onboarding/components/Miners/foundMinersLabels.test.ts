import { describe, expect, it } from "vitest";
import { deviceGroupLabel, entityNoun, fleetSourceLabel, foundSummary } from "./foundMinersLabels";
import { DeviceClass } from "@/protoFleet/api/generated/common/v1/common_pb";

describe("entityNoun", () => {
  it('pluralizes container groups as "container"/"containers"', () => {
    expect(entityNoun(DeviceClass.CONTAINER, 1)).toBe("container");
    expect(entityNoun(DeviceClass.CONTAINER, 2)).toBe("containers");
  });

  it('pluralizes rig groups as "miner"/"miners"', () => {
    expect(entityNoun(DeviceClass.RIG, 1)).toBe("miner");
    expect(entityNoun(DeviceClass.RIG, 3)).toBe("miners");
  });
});

describe("deviceGroupLabel", () => {
  it("does not infer containers from CU-prefixed rig or unspecified models", () => {
    expect(deviceGroupLabel("Proto", "CU1", DeviceClass.RIG)).toBe("Proto CU1");
    expect(deviceGroupLabel("Proto", "CU1", DeviceClass.UNSPECIFIED)).toBe("Proto CU1");
  });
  it('labels container groups "Proto Container"', () => {
    expect(deviceGroupLabel("Proto", "CT1", DeviceClass.CONTAINER)).toBe("Proto Container");
    expect(deviceGroupLabel("Proto", "Any model", DeviceClass.CONTAINER)).toBe("Proto Container");
  });

  it("drops a redundant manufacturer prefix already present in the model", () => {
    expect(deviceGroupLabel("Proto", "Proto Rig", DeviceClass.RIG)).toBe("Proto Rig");
    expect(deviceGroupLabel("proto", "Proto Rig", DeviceClass.RIG)).toBe("Proto Rig");
  });

  it("joins manufacturer and model when they differ", () => {
    expect(deviceGroupLabel("Bitmain", "Antminer S21", DeviceClass.RIG)).toBe("Bitmain Antminer S21");
  });

  it("only dedupes on a whole-word prefix, not a partial match", () => {
    expect(deviceGroupLabel("Proto", "Protocol Miner", DeviceClass.RIG)).toBe("Proto Protocol Miner");
  });

  it("handles missing manufacturer or model", () => {
    expect(deviceGroupLabel("", "Antminer S21", DeviceClass.RIG)).toBe("Antminer S21");
    expect(deviceGroupLabel("Bitmain", "", DeviceClass.RIG)).toBe("Bitmain");
    expect(deviceGroupLabel("Proto", "Proto", DeviceClass.RIG)).toBe("Proto");
  });
});

describe("foundSummary", () => {
  it("splits the total by entity when both buckets are present", () => {
    expect(foundSummary(48, 300)).toBe("48 containers and 300 miners found on your network");
  });

  it("uses singular nouns for single-item buckets", () => {
    expect(foundSummary(1, 1)).toBe("1 container and 1 miner found on your network");
  });

  it("omits the container bucket when there are no containers (backward compatible)", () => {
    // Preserves the original "N miners found on your network" wording relied on
    // by MinersWrapper.test.tsx.
    expect(foundSummary(0, 4)).toBe("4 miners found on your network");
    expect(foundSummary(0, 1)).toBe("1 miner found on your network");
  });

  it("omits the miner bucket when there are only containers", () => {
    expect(foundSummary(2, 0)).toBe("2 containers found on your network");
    expect(foundSummary(1, 0)).toBe("1 container found on your network");
  });
});

describe("fleetSourceLabel", () => {
  it.each([
    ["0", "Ignored", "Fleet Server"],
    ["7", "Mining hall", "Mining hall"],
    ["7", "", "Fleet Node 7"],
    ["7", undefined, "Fleet Node 7"],
  ])("labels node %s with name %s as %s", (id, name, expected) => {
    const label = fleetSourceLabel(id, name);

    expect(label).toBe(expected);
  });
});
