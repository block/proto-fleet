import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";

import {
  type Rollout,
  RolloutDeviceState,
  type RolloutLane,
  RolloutLaneSchema,
  RolloutSchema,
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
  createdAt: minutesAgo(60 * 27),
  finishedAt: minutesAgo(60 * 26),
  devices: [],
});

// Newest first, matching the server's ListRollouts order.
export const canaryHistory: Rollout[] = [activeRigRollout, completedRigRollout, canceledRigRollout];
