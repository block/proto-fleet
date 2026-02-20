import { ColTitles } from "@/shared/components/List/types";

export const MINERS_PAGE_SIZE = 50;

export const minerCols = {
  name: "name",
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
} as const;

export type MinerColumn = (typeof minerCols)[keyof typeof minerCols];

export const minerColTitles: ColTitles<MinerColumn> = {
  name: "Name",
  model: "Model",
  macAddress: "MAC Address",
  ipAddress: "IP Address",
  status: "Status",
  issues: "Issues",
  hashrate: "Hashrate",
  efficiency: "Efficiency",
  powerUsage: "Power",
  temperature: "Temp",
  firmware: "Firmware",
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

/** Placeholder text displayed when a miner list value is unavailable */
export const INACTIVE_PLACEHOLDER = "—";
