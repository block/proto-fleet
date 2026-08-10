import { describe, expect, test } from "vitest";
import { create } from "@bufbuild/protobuf";

import { buildRackCreationSuggestion, RACK_SUGGESTION_MAX_MINERS, RACK_SUGGESTION_MIN_MINERS } from "./rackSuggestion";
import {
  type MinerStateSnapshot,
  MinerStateSnapshotSchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

const miner = (id: number, ipAddress: string, model = "Proto Rig"): MinerStateSnapshot =>
  create(MinerStateSnapshotSchema, {
    deviceIdentifier: `miner-${id}`,
    ipAddress,
    model,
  });

describe("buildRackCreationSuggestion", () => {
  test("groups nearby unassigned miners by IPv4 range", () => {
    const miners = Array.from({ length: RACK_SUGGESTION_MIN_MINERS }, (_, i) => miner(i + 1, `10.90.12.${40 + i}`));

    expect(buildRackCreationSuggestion(miners)).toEqual({
      count: RACK_SUGGESTION_MIN_MINERS,
      minerIds: miners.map((m) => m.deviceIdentifier),
      ipRangeLabel: "10.90.12.40-10.90.12.47",
      ipRangeFilter: "10.90.12.40-10.90.12.47",
      modelSummary: "All Proto Rig.",
      dismissalKey: `rack:10.90.12.40:10.90.12.47:${RACK_SUGGESTION_MIN_MINERS}`,
    });
  });

  test("chooses the largest plausible cohort", () => {
    const small = Array.from({ length: RACK_SUGGESTION_MIN_MINERS }, (_, i) => miner(i + 1, `10.90.12.${10 + i}`));
    const large = Array.from({ length: RACK_SUGGESTION_MIN_MINERS + 2 }, (_, i) =>
      miner(100 + i, `10.90.13.${30 + i}`, i === 0 ? "Antminer S21" : "Proto Rig"),
    );

    const suggestion = buildRackCreationSuggestion([...small, ...large]);

    expect(suggestion?.count).toBe(RACK_SUGGESTION_MIN_MINERS + 2);
    expect(suggestion?.ipRangeLabel).toBe("10.90.13.30-10.90.13.39");
    expect(suggestion?.modelSummary).toBe("Mostly Proto Rig.");
  });

  test("does not suggest tiny, oversized, or non-ip cohorts", () => {
    const tiny = Array.from({ length: RACK_SUGGESTION_MIN_MINERS - 1 }, (_, i) => miner(i + 1, `10.90.12.${40 + i}`));
    const oversized = Array.from({ length: RACK_SUGGESTION_MAX_MINERS + 1 }, (_, i) =>
      miner(i + 1, `10.90.12.${i + 1}`),
    );

    expect(buildRackCreationSuggestion(tiny)).toBeUndefined();
    expect(buildRackCreationSuggestion(oversized)).toBeUndefined();
    expect(buildRackCreationSuggestion([miner(1, "miner.local")])).toBeUndefined();
  });
});
