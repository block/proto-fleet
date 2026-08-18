import { describe, expect, it } from "vitest";

import { buildManualBatches, evaluateTargetCompatibility } from "./betweenChannelUtils";
import { minerTargetKey } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";
import type { RolloutLaneReleaseTarget } from "@/protoFleet/features/rollout/rolloutTypes";

const sourceTargets: RolloutLaneReleaseTarget[] = [
  {
    firmwareFileId: "alpha-1",
    targetManufacturer: "Proto",
    targetModel: "Alpha",
    firmwareVersion: "1.0.0",
    sha256: "a",
  },
  {
    firmwareFileId: "beta-1",
    targetManufacturer: "Proto",
    targetModel: "Beta",
    firmwareVersion: "2.0.0",
    sha256: "b",
  },
];

const files = [
  {
    id: "alpha-1",
    filename: "alpha-1.swu",
    size: 100,
    uploaded_at: "2026-08-18T00:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Alpha",
    firmware_version: "1.0.0",
  },
  {
    id: "alpha-2",
    filename: "alpha-2.swu",
    size: 100,
    uploaded_at: "2026-08-18T00:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Alpha",
    firmware_version: "2.0.0",
  },
  {
    id: "beta-3",
    filename: "beta-3.swu",
    size: 100,
    uploaded_at: "2026-08-18T00:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Beta",
    firmware_version: "3.0.0",
  },
];

function targetKey(manufacturer: string, model: string): string {
  const key = minerTargetKey(manufacturer, model);
  if (key === null) {
    throw new Error("Test target must be complete.");
  }
  return key;
}

describe("between-channel rollout helpers", () => {
  it("blocks a target that is missing a source model", () => {
    const rows = evaluateTargetCompatibility(sourceTargets, files, {
      [targetKey("Proto", "Alpha")]: "alpha-2",
    });

    expect(rows).toEqual([
      expect.objectContaining({ model: "Alpha", status: "compatible" }),
      expect.objectContaining({ model: "Beta", status: "missing" }),
    ]);
  });

  it("blocks a no-op target release", () => {
    const rows = evaluateTargetCompatibility(sourceTargets, files, {
      [targetKey("Proto", "Alpha")]: "alpha-1",
      [targetKey("Proto", "Beta")]: "beta-3",
    });

    expect(rows[0]).toMatchObject({
      sourceVersion: "1.0.0",
      targetVersion: "1.0.0",
      status: "noOp",
    });
  });

  it("builds deterministic pilot and manual batch assignments", () => {
    const members = ["miner-3", "miner-1", "miner-2", "miner-4"];

    expect(buildManualBatches(members, { strategy: "pilotThenContinue", pilotSize: 1 })).toEqual([
      { label: "Pilot", members: [{ deviceIdentifier: "miner-3" }] },
      {
        label: "Remaining",
        members: [{ deviceIdentifier: "miner-1" }, { deviceIdentifier: "miner-2" }, { deviceIdentifier: "miner-4" }],
      },
    ]);
    expect(buildManualBatches(members, { strategy: "batched", batchSize: 2 })).toEqual([
      {
        label: "Batch 1",
        members: [{ deviceIdentifier: "miner-3" }, { deviceIdentifier: "miner-1" }],
      },
      {
        label: "Batch 2",
        members: [{ deviceIdentifier: "miner-2" }, { deviceIdentifier: "miner-4" }],
      },
    ]);
  });
});
