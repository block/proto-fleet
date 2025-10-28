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

export const minerFilterStates = {
  hashing: "hashing",
  broken: "broken",
  offline: "offline",
  asleep: "asleep",
};

export type MinerFilterState =
  (typeof minerFilterStates)[keyof typeof minerFilterStates];

export const minerTypes = {
  protoRig: "proto",
  bitmain: "bitmain",
};
