import { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { ColTitles } from "@/shared/components/List/types";

export const minerCols = {
  name: "name",
  macAddress: "macAddress",
  ipAddress: "ipAddress",
  status: "status",
  hashrate: "hashrate",
  efficiency: "efficiency",
  powerUsage: "powerUsage",
  temperature: "temperature",
};

export const minerColTitles = {
  [minerCols.name]: "Name",
  [minerCols.macAddress]: "Mac Address",
  [minerCols.ipAddress]: "IP Address",
  [minerCols.status]: "Status",
  [minerCols.hashrate]: "Hashrate",
  [minerCols.efficiency]: "Efficiency",
  [minerCols.powerUsage]: "Power",
  [minerCols.temperature]: "Temp",
} as ColTitles<keyof MinerStateSnapshot>;

export const deviceStatusFilterStates = {
  hashing: "hashing",
  offline: "offline",
  sleeping: "sleeping",
  needsAttention: "needsAttention",
};

export type DeviceStatusFilterState =
  (typeof deviceStatusFilterStates)[keyof typeof deviceStatusFilterStates];

export const minerTypes = {
  protoRig: "proto",
  bitmain: "bitmain",
};

export const componentIssues = {
  controlBoard: "control-board",
  fans: "fans",
  hashBoards: "hash-boards",
  psu: "psu",
};
