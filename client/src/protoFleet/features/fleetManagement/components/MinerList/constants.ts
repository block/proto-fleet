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
} as const;

export type MinerColumn = (typeof minerCols)[keyof typeof minerCols];

export const minerColTitles: ColTitles<MinerColumn> = {
  name: "Name",
  macAddress: "Mac Address",
  ipAddress: "IP Address",
  status: "Status",
  hashrate: "Hashrate",
  efficiency: "Efficiency",
  powerUsage: "Power",
  temperature: "Temp",
};

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
