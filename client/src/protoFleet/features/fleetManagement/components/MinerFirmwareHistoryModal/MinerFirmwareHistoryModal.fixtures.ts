import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";

import {
  MinerFirmwareHistoryEntrySchema,
  RolloutDevicePhase,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

export const completedHistoryEntry = create(MinerFirmwareHistoryEntrySchema, {
  rolloutId: 42n,
  channelId: 3n,
  channelName: "Production",
  manufacturer: "Proto",
  model: "Rig",
  firmwareVersion: "1.4.3",
  firmwareChecksum: "sha256:firmware-143",
  rolloutStatus: RolloutStatus.COMPLETED_WITH_FAILURES,
  phase: RolloutDevicePhase.DONE,
  attempts: 2,
  createdAt: timestampFromDate(new Date("2026-09-26T10:00:00Z")),
  lastSentAt: timestampFromDate(new Date("2026-09-26T10:05:00Z")),
  verifiedAt: timestampFromDate(new Date("2026-09-26T10:08:00Z")),
  finishedAt: timestampFromDate(new Date("2026-09-26T11:00:00Z")),
});

export const historyEntries = [
  completedHistoryEntry,
  create(MinerFirmwareHistoryEntrySchema, {
    ...completedHistoryEntry,
    rolloutId: 40n,
    firmwareVersion: "1.4.2",
    phase: RolloutDevicePhase.FAILED,
    lastError: "Firmware did not match the target version after 3 attempts.",
    attempts: 3,
    verifiedAt: undefined,
  }),
  create(MinerFirmwareHistoryEntrySchema, {
    ...completedHistoryEntry,
    rolloutId: 39n,
    firmwareVersion: "1.4.1",
    rolloutStatus: RolloutStatus.CANCELED,
    phase: RolloutDevicePhase.IN_PROGRESS,
    attempts: 1,
    verifiedAt: undefined,
  }),
];
