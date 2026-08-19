import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import { BETWEEN_CHANNEL_STRATEGY_KEY } from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import type { RolloutLane, RolloutMember, RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";

const member = (
  id: bigint,
  deviceIdentifier: string,
  state: RolloutMember["state"],
  batchId: bigint,
  lastError?: string,
): RolloutMember => ({
  id,
  batchId,
  deviceIdentifier,
  position: Number(id - 1n),
  state,
  revision: 1n,
  lastError,
  evidence: [],
});

export const betweenChannelFiles: FirmwareFileInfo[] = [
  {
    id: "alpha-1",
    filename: "proto-alpha-1.0.0.swu",
    size: 1024,
    uploaded_at: "2026-08-17T12:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Alpha",
    firmware_version: "1.0.0",
  },
  {
    id: "alpha-2",
    filename: "proto-alpha-2.0.0.swu",
    size: 1024,
    uploaded_at: "2026-08-18T12:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Alpha",
    firmware_version: "2.0.0",
  },
];

export const stableProductionLane: RolloutLane = {
  id: "15bc6181-07d8-45ac-8424-50b5e938b871",
  label: "Stable production",
  description: "Production firmware lane",
  currentChannelId: 41n,
  revision: 2n,
  channels: [
    {
      channelId: 41n,
      releaseSetId: 7n,
      position: 0,
      current: true,
    },
  ],
  memberCount: 3,
  memberIdentifiers: ["miner-1", "miner-2", "miner-3"],
  firmwareConvergence: {
    totalCount: 3,
    pendingCount: 0,
    updatingCount: 0,
    verifyingCount: 0,
    confirmedCount: 3,
    attentionCount: 0,
    members: [],
  },
  currentReleaseTargets: [
    {
      firmwareFileId: "alpha-1",
      targetManufacturer: "Proto",
      targetModel: "Alpha",
      firmwareVersion: "1.0.0",
      sha256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    },
  ],
};

export const attentionRequiredRollout: RolloutRecord = {
  id: "2f214a71-f94e-4e5f-8daf-d36c71b72f6c",
  name: "Production 2.0.0",
  strategyKey: BETWEEN_CHANNEL_STRATEGY_KEY,
  state: "running",
  revision: 4n,
  reason: "Validated release",
  startedAt: "2026-08-18T12:00:00Z",
  batches: [
    {
      id: 1n,
      position: 0,
      label: "Pilot",
      state: "admitted",
      revision: 1n,
      members: [],
    },
    {
      id: 2n,
      position: 1,
      label: "Remaining",
      state: "pending",
      revision: 1n,
      members: [],
    },
  ],
  members: [
    member(1n, "miner-1", "succeeded", 1n),
    member(2n, "miner-2", "attentionRequired", 1n, "Firmware result is ambiguous"),
    member(3n, "miner-3", "pending", 2n),
  ],
  causes: [],
  availableActions: {
    admit: true,
    continue: false,
    pause: true,
    resume: false,
    abort: true,
    revert: false,
    complete: true,
  },
};

export const abortedRollout: RolloutRecord = {
  ...attentionRequiredRollout,
  state: "aborted",
  revision: 5n,
  members: [
    member(1n, "miner-1", "succeeded", 1n),
    member(2n, "miner-2", "cancelled", 1n),
    member(3n, "miner-3", "cancelled", 2n),
  ],
  availableActions: {
    admit: false,
    continue: false,
    pause: false,
    resume: false,
    abort: false,
    revert: true,
    complete: false,
  },
};
