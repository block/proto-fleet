import type { ColTitles } from "@/shared/components/List/types";

export const deviceSetCols = {
  name: "name",
  zone: "zone",
  miners: "miners",
  issues: "issues",
  hashrate: "hashrate",
  efficiency: "efficiency",
  power: "power",
  temperature: "temperature",
  health: "health",
} as const;

export type DeviceSetColumn = (typeof deviceSetCols)[keyof typeof deviceSetCols];

export const deviceSetColTitles: ColTitles<DeviceSetColumn> = {
  name: "Name",
  zone: "Zone",
  miners: "Miners",
  issues: "Issues",
  hashrate: "Total Hashrate",
  efficiency: "Avg Efficiency",
  power: "Total Power",
  temperature: "Temperature",
  health: "Health",
};

export const DEFAULT_PAGE_SIZE = 50;
