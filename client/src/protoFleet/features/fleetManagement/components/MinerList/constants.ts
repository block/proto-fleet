import { ColTitles } from "@/shared/components/List/types";
export { INACTIVE_PLACEHOLDER } from "@/shared/constants";

export const MINERS_PAGE_SIZE = 50;

export const minerCols = {
  name: "name",
  workerName: "workerName",
  model: "model",
  macAddress: "macAddress",
  ipAddress: "ipAddress",
  status: "status",
  issues: "issues",
  hashrate: "hashrate",
  efficiency: "efficiency",
  powerUsage: "powerUsage",
  temperature: "temperature",
  firmware: "firmware",
  groups: "groups",
} as const;

export type MinerColumn = (typeof minerCols)[keyof typeof minerCols];

export const minerColTitles: ColTitles<MinerColumn> = {
  name: "Name",
  workerName: "Worker name",
  model: "Model",
  macAddress: "MAC address",
  ipAddress: "IP address",
  status: "Status",
  issues: "Issues",
  hashrate: "Hashrate",
  efficiency: "Efficiency",
  powerUsage: "Power",
  temperature: "Temp",
  firmware: "Firmware",
  groups: "Groups",
};

export const deviceStatusFilterStates = {
  hashing: "hashing",
  offline: "offline",
  sleeping: "sleeping",
  needsAttention: "needsAttention",
};

export type DeviceStatusFilterState = (typeof deviceStatusFilterStates)[keyof typeof deviceStatusFilterStates];

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
