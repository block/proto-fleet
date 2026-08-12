import { describe, expect, it } from "vitest";
import { deviceGroupLabel, entityNoun, foundSummary, isContainerModel } from "./foundMinersLabels";

describe("isContainerModel", () => {
  it('treats models starting with "CU" as containers (case-insensitive)', () => {
    expect(isContainerModel("CU1")).toBe(true);
    expect(isContainerModel("cu-200")).toBe(true);
    expect(isContainerModel("Cu2")).toBe(true);
  });

  it("treats rig models as non-containers", () => {
    expect(isContainerModel("Antminer S21")).toBe(false);
    expect(isContainerModel("Proto Rig")).toBe(false);
    expect(isContainerModel("")).toBe(false);
  });

  it("is safe for non-string input", () => {
    // Discovery data can be malformed; guard against runtime nulls.
    expect(isContainerModel(undefined as unknown as string)).toBe(false);
  });
});

describe("entityNoun", () => {
  it('pluralizes container groups as "container"/"containers"', () => {
    expect(entityNoun("CU1", 1)).toBe("container");
    expect(entityNoun("CU1", 2)).toBe("containers");
  });

  it('pluralizes rig groups as "miner"/"miners"', () => {
    expect(entityNoun("Antminer S21", 1)).toBe("miner");
    expect(entityNoun("Antminer S21", 3)).toBe("miners");
  });
});

describe("deviceGroupLabel", () => {
  it('labels container groups "Proto Container"', () => {
    expect(deviceGroupLabel("Proto", "CU1")).toBe("Proto Container");
    expect(deviceGroupLabel("Proto", "cu-200")).toBe("Proto Container");
  });

  it("drops a redundant manufacturer prefix already present in the model", () => {
    expect(deviceGroupLabel("Proto", "Proto Rig")).toBe("Proto Rig");
    expect(deviceGroupLabel("proto", "Proto Rig")).toBe("Proto Rig");
  });

  it("joins manufacturer and model when they differ", () => {
    expect(deviceGroupLabel("Bitmain", "Antminer S21")).toBe("Bitmain Antminer S21");
  });

  it("only dedupes on a whole-word prefix, not a partial match", () => {
    expect(deviceGroupLabel("Proto", "Protocol Miner")).toBe("Proto Protocol Miner");
  });

  it("handles missing manufacturer or model", () => {
    expect(deviceGroupLabel("", "Antminer S21")).toBe("Antminer S21");
    expect(deviceGroupLabel("Bitmain", "")).toBe("Bitmain");
    expect(deviceGroupLabel("Proto", "Proto")).toBe("Proto");
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
