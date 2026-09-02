import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";

import {
  type Rollout,
  RolloutCancelReason,
  RolloutDeviceState,
  RolloutEvidenceSchema,
  type RolloutLane,
  RolloutLaneSchema,
  RolloutMethod,
  RolloutSchema,
  RolloutStage,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";

// Fixture data for the rollout channel stories: one mixed-fleet channel
// ("Canary") mid-rollout on its Rig group, plus the firmware files and
// display names the components take as props.

const minutesAgo = (minutes: number) => timestampFromDate(new Date(Date.now() - minutes * 60_000));

export const firmwareFiles: FirmwareFileInfo[] = [
  {
    id: "fw-rig-143",
    filename: "proto-rig-1.4.3.swu",
    size: 52_428_800,
    uploaded_at: "2026-08-20T09:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Rig",
    firmware_version: "1.4.3",
  },
  {
    id: "fw-rig-144",
    filename: "proto-rig-1.4.4.swu",
    size: 52_690_944,
    uploaded_at: "2026-08-27T14:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Rig",
    firmware_version: "1.4.4",
  },
  {
    id: "fw-s21-301",
    filename: "s21-3.0.1.bin",
    size: 33_554_432,
    uploaded_at: "2026-08-25T11:30:00Z",
    target_manufacturer: "Bitmain",
    target_model: "S21",
    firmware_version: "3.0.1",
  },
];

export const minerNames: Record<string, string> = {
  "rig-001": "Rig A01",
  "rig-002": "Rig A02",
  "rig-003": "Rig A03",
  "rig-004": "Rig B01",
  "rig-005": "Rig B02",
  "rig-006": "Rig B03",
  "s21-001": "S21 Rack 4-1",
  "s21-002": "S21 Rack 4-2",
};

const rigMiner = (n: number, firmwareVersion: string) => ({
  deviceId: BigInt(100 + n),
  deviceIdentifier: `rig-00${n}`,
  model: "Rig",
  firmwareVersion,
});

// "Canary" holds two model groups: Rig is assigned 1.4.4 and partway
// through updating, S21 has no assignment yet.
export const canaryChannel: RolloutLane = create(RolloutLaneSchema, {
  id: BigInt(1),
  name: "Canary",
  createdAt: minutesAgo(60 * 24 * 3),
  modelGroups: [
    {
      model: "Rig",
      firmwareFileId: "fw-rig-144",
      firmwareVersion: "1.4.4",
      activeRolloutId: BigInt(41),
      miners: [
        rigMiner(1, "1.4.4"),
        rigMiner(2, "1.4.4"),
        rigMiner(3, "1.4.3"),
        rigMiner(4, "1.4.3"),
        rigMiner(5, "1.4.3"),
        rigMiner(6, "1.4.3"),
      ],
    },
    {
      model: "S21",
      firmwareFileId: "",
      firmwareVersion: "",
      activeRolloutId: BigInt(0),
      miners: [
        { deviceId: BigInt(201), deviceIdentifier: "s21-001", model: "S21", firmwareVersion: "3.0.0" },
        { deviceId: BigInt(202), deviceIdentifier: "s21-002", model: "S21", firmwareVersion: "3.0.0" },
      ],
    },
  ],
});

// The same channel with the Rig rollout finished and every miner compliant.
export const canaryChannelSettled: RolloutLane = create(RolloutLaneSchema, {
  ...canaryChannel,
  modelGroups: canaryChannel.modelGroups.map((group) =>
    group.model === "Rig"
      ? {
          ...group,
          activeRolloutId: BigInt(0),
          miners: group.miners.map((miner) => ({ ...miner, firmwareVersion: "1.4.4" })),
        }
      : group,
  ),
});

export const emptyChannel: RolloutLane = create(RolloutLaneSchema, {
  id: BigInt(2),
  name: "Staging",
  createdAt: minutesAgo(30),
  modelGroups: [],
});

// A settled channel with a distinct id, for stories listing several channels.
export const productionChannel: RolloutLane = create(RolloutLaneSchema, {
  ...canaryChannelSettled,
  id: BigInt(3),
  name: "Production",
});

// Ongoing enforcement of 1.4.4 on the Rig group: two miners done, one
// mid-flash, three queued.
export const activeRigRollout: Rollout = create(RolloutSchema, {
  id: BigInt(41),
  laneId: BigInt(1),
  laneName: "Canary",
  model: "Rig",
  firmwareFileId: "fw-rig-144",
  firmwareVersion: "1.4.4",
  status: RolloutStatus.ACTIVE,
  method: RolloutMethod.IMMEDIATE,
  stage: RolloutStage.REST,
  createdAt: minutesAgo(4),
  devices: [
    { deviceId: BigInt(101), deviceIdentifier: "rig-001", firmwareVersion: "1.4.4", state: RolloutDeviceState.UPDATED },
    { deviceId: BigInt(102), deviceIdentifier: "rig-002", firmwareVersion: "1.4.4", state: RolloutDeviceState.UPDATED },
    {
      deviceId: BigInt(103),
      deviceIdentifier: "rig-003",
      firmwareVersion: "1.4.3",
      state: RolloutDeviceState.UPDATING,
    },
    { deviceId: BigInt(104), deviceIdentifier: "rig-004", firmwareVersion: "1.4.3", state: RolloutDeviceState.PENDING },
    { deviceId: BigInt(105), deviceIdentifier: "rig-005", firmwareVersion: "1.4.3", state: RolloutDeviceState.PENDING },
    { deviceId: BigInt(106), deviceIdentifier: "rig-006", firmwareVersion: "1.4.3", state: RolloutDeviceState.PENDING },
  ],
});

export const nearlyDoneRigRollout: Rollout = create(RolloutSchema, {
  ...activeRigRollout,
  id: BigInt(42),
  createdAt: minutesAgo(38),
  devices: activeRigRollout.devices.map((device, index) => ({
    ...device,
    firmwareVersion: index < 5 ? "1.4.4" : "1.4.3",
    state: index < 5 ? RolloutDeviceState.UPDATED : RolloutDeviceState.UPDATING,
  })),
});

// A second concurrent rollout in another channel, for the header pill.
export const activeS19Rollout: Rollout = create(RolloutSchema, {
  id: BigInt(43),
  laneId: BigInt(3),
  laneName: "Production",
  model: "S19",
  firmwareFileId: "fw-s19-210",
  firmwareVersion: "2.1.0",
  status: RolloutStatus.ACTIVE,
  createdAt: minutesAgo(12),
  devices: [
    {
      deviceId: BigInt(301),
      deviceIdentifier: "s19-001",
      firmwareVersion: "2.0.4",
      state: RolloutDeviceState.UPDATING,
    },
    { deviceId: BigInt(302), deviceIdentifier: "s19-002", firmwareVersion: "2.0.4", state: RolloutDeviceState.PENDING },
    { deviceId: BigInt(303), deviceIdentifier: "s19-003", firmwareVersion: "2.1.0", state: RolloutDeviceState.UPDATED },
  ],
});

export const completedRigRollout: Rollout = create(RolloutSchema, {
  id: BigInt(40),
  laneId: BigInt(1),
  laneName: "Canary",
  model: "Rig",
  firmwareFileId: "fw-rig-143",
  firmwareVersion: "1.4.3",
  status: RolloutStatus.COMPLETED,
  createdAt: minutesAgo(60 * 26),
  finishedAt: minutesAgo(60 * 25),
  devices: activeRigRollout.devices.map((device) => ({
    ...device,
    firmwareVersion: "1.4.3",
    state: RolloutDeviceState.UPDATED,
  })),
});

export const canceledRigRollout: Rollout = create(RolloutSchema, {
  id: BigInt(39),
  laneId: BigInt(1),
  laneName: "Canary",
  model: "Rig",
  firmwareFileId: "fw-rig-142",
  firmwareVersion: "1.4.2",
  status: RolloutStatus.CANCELED,
  cancelReason: RolloutCancelReason.SUPERSEDED,
  createdAt: minutesAgo(60 * 27),
  finishedAt: minutesAgo(60 * 26),
  devices: [],
});

// A rollout an operator aborted at the review gate: the previous assignment
// (1.4.3) was restored.
export const abortedRigRollout: Rollout = create(RolloutSchema, {
  id: BigInt(38),
  laneId: BigInt(1),
  laneName: "Canary",
  model: "Rig",
  firmwareFileId: "fw-rig-144",
  firmwareVersion: "1.4.4",
  previousFirmwareFileId: "fw-rig-143",
  previousFirmwareVersion: "1.4.3",
  status: RolloutStatus.CANCELED,
  cancelReason: RolloutCancelReason.ABORTED,
  method: RolloutMethod.PILOT,
  stage: RolloutStage.AWAITING_REVIEW,
  batchSize: 1,
  batchCount: 1,
  createdAt: minutesAgo(60 * 48),
  finishedAt: minutesAgo(60 * 47),
  devices: [],
});

// Health fields for a miner as the server reports them: live status and
// hashrate alongside the baseline captured when the rollout started.
const TH = 1e12;
const healthy = (hashRateTh: number, baselineTh = hashRateTh) => ({
  status: "ACTIVE",
  online: true,
  hashing: true,
  hasBaseline: true,
  baselineHashing: true,
  hashRateHs: hashRateTh * TH,
  hasHashRate: true,
  baselineHashRateHs: baselineTh * TH,
  hasBaselineHashRate: true,
  openErrors: 0,
  baselineOpenErrors: 0,
});
const offline = (baselineTh: number) => ({
  ...healthy(0, baselineTh),
  status: "OFFLINE",
  online: false,
  hashing: false,
  hasHashRate: false,
});

// A pilot rollout mid-pilot: the two-miner batch is updating (one back and
// hashing, one still flashing), the rest of the group waits for the gate.
export const pilotRigRollout: Rollout = create(RolloutSchema, {
  ...activeRigRollout,
  id: BigInt(44),
  method: RolloutMethod.PILOT,
  stage: RolloutStage.BATCH,
  batchSize: 2,
  batchCount: 1,
  currentBatch: 0,
  stageChangedAt: minutesAgo(9),
  createdAt: minutesAgo(9),
  devices: activeRigRollout.devices.map((device, index) => ({
    ...device,
    ...(index === 1 ? offline(112) : healthy(index === 0 ? 114 : 112)),
    batch: index < 2 ? 1 : 0,
    firmwareVersion: index === 0 ? "1.4.4" : "1.4.3",
    state:
      index === 0 ? RolloutDeviceState.UPDATED : index === 1 ? RolloutDeviceState.UPDATING : RolloutDeviceState.PENDING,
  })),
  evidence: create(RolloutEvidenceSchema, {
    devicesTotal: 2,
    verified: 1,
    online: 1,
    hashing: 1,
    baselineHashing: 2,
    hasHashrateEvidence: true,
    baselineHashRateHs: 112 * TH,
    currentHashRateHs: 114 * TH,
    hashrateChangePercent: 1.8,
    newErrors: 0,
    readyToAdvance: false,
    holdReason: "Batch in progress",
  }),
});

// The same pilot rollout parked at the review gate with healthy evidence:
// both pilot miners are back and hashing slightly above baseline.
export const gatedRigRollout: Rollout = create(RolloutSchema, {
  ...pilotRigRollout,
  id: BigInt(45),
  stage: RolloutStage.AWAITING_REVIEW,
  stageChangedAt: minutesAgo(6),
  createdAt: minutesAgo(22),
  devices: pilotRigRollout.devices.map((device) => ({
    ...device,
    ...healthy(device.batch === 1 ? 115 : 112, 112),
    firmwareVersion: device.batch === 1 ? "1.4.4" : "1.4.3",
    state: device.batch === 1 ? RolloutDeviceState.UPDATED : RolloutDeviceState.PENDING,
  })),
  evidence: create(RolloutEvidenceSchema, {
    devicesTotal: 2,
    verified: 2,
    online: 2,
    hashing: 2,
    baselineHashing: 2,
    hasHashrateEvidence: true,
    baselineHashRateHs: 224 * TH,
    currentHashRateHs: 230 * TH,
    hashrateChangePercent: 2.7,
    newErrors: 0,
    readyToAdvance: false,
    holdReason: "Manual review",
  }),
});

// Fixed batches of two with auto-advance, holding at the gate after batch 2
// of 3 because one miner came back hashing well below its baseline.
export const batchedRigRollout: Rollout = create(RolloutSchema, {
  ...activeRigRollout,
  id: BigInt(46),
  method: RolloutMethod.BATCHES,
  stage: RolloutStage.AWAITING_REVIEW,
  batchSize: 2,
  batchCount: 3,
  currentBatch: 1,
  autoAdvance: true,
  maxHashrateDropPercent: 10,
  stabilizationSeconds: 600,
  stageChangedAt: minutesAgo(14),
  createdAt: minutesAgo(41),
  devices: activeRigRollout.devices.map((device, index) => ({
    ...device,
    ...healthy(index === 3 ? 61 : 112, 112),
    batch: Math.floor(index / 2) + 1,
    firmwareVersion: index < 4 ? "1.4.4" : "1.4.3",
    state: index < 4 ? RolloutDeviceState.UPDATED : RolloutDeviceState.PENDING,
    openErrors: index === 3 ? 2 : 0,
  })),
  evidence: create(RolloutEvidenceSchema, {
    devicesTotal: 2,
    verified: 2,
    online: 2,
    hashing: 2,
    baselineHashing: 2,
    hasHashrateEvidence: true,
    baselineHashRateHs: 224 * TH,
    currentHashRateHs: 173 * TH,
    hashrateChangePercent: -22.8,
    newErrors: 2,
    readyToAdvance: false,
    holdReason: "2 new errors since the update",
  }),
});

// An immediate rollout an operator paused mid-flight.
export const pausedRigRollout: Rollout = create(RolloutSchema, {
  ...activeRigRollout,
  id: BigInt(47),
  pausedAt: minutesAgo(3),
  createdAt: minutesAgo(11),
  devices: activeRigRollout.devices.map((device, index) => ({
    ...device,
    ...(index === 2 ? offline(112) : healthy(112)),
  })),
  evidence: create(RolloutEvidenceSchema, {
    devicesTotal: 6,
    verified: 2,
    online: 5,
    hashing: 5,
    baselineHashing: 6,
    hasHashrateEvidence: true,
    baselineHashRateHs: 672 * TH,
    currentHashRateHs: 560 * TH,
    hashrateChangePercent: -16.7,
    newErrors: 0,
    readyToAdvance: false,
    holdReason: "Paused",
  }),
});

// Newest first, matching the server's ListRollouts order.
export const canaryHistory: Rollout[] = [activeRigRollout, completedRigRollout, canceledRigRollout, abortedRigRollout];
