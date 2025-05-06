import { type Miner } from "./types";
import { ColTitles } from "@/shared/components/List/types";

export const minerCols = {
  name: "name",
  macAddress: "macAddress",
  status: "status",
  hashrate: "hashrate",
  efficiency: "efficiency",
  powerUsage: "powerUsage",
  temperature: "temperature",
};

export const minerColTitles = {
  [minerCols.name]: "Name",
  [minerCols.macAddress]: "Mac Address",
  [minerCols.status]: "Status",
  [minerCols.hashrate]: "Hashrate",
  [minerCols.efficiency]: "Efficiency",
  [minerCols.powerUsage]: "Power Usage",
  [minerCols.temperature]: "Temperature",
} as ColTitles<keyof Miner>;

export const minerFilterStates = {
  hashing: "hashing",
  broken: "broken",
  offline: "offline",
  asleep: "asleep",
};

export type MinerFilterState =
  (typeof minerFilterStates)[keyof typeof minerFilterStates];
