import { ColTitles } from "@/shared/components/List/types";

export const GROUPS_PAGE_SIZE = 50;

export const groupCols = {
  name: "name",
  miners: "miners",
  issues: "issues",
  hashrate: "hashrate",
  efficiency: "efficiency",
  power: "power",
  temperature: "temperature",
  health: "health",
} as const;

export type GroupColumn = (typeof groupCols)[keyof typeof groupCols];

export const groupColTitles: ColTitles<GroupColumn> = {
  name: "Name",
  miners: "Miners",
  issues: "Issues",
  hashrate: "Total Hashrate",
  efficiency: "Avg Efficiency",
  power: "Total Power",
  temperature: "Temperature",
  health: "Health",
};
