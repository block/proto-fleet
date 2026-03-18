import type { ColTitles } from "@/shared/components/List/types";

export const collectionCols = {
  name: "name",
  miners: "miners",
  issues: "issues",
  hashrate: "hashrate",
  efficiency: "efficiency",
  power: "power",
  temperature: "temperature",
  health: "health",
} as const;

export type CollectionColumn = (typeof collectionCols)[keyof typeof collectionCols];

export const collectionColTitles: ColTitles<CollectionColumn> = {
  name: "Name",
  miners: "Miners",
  issues: "Issues",
  hashrate: "Total Hashrate",
  efficiency: "Avg Efficiency",
  power: "Total Power",
  temperature: "Temperature",
  health: "Health",
};

export const DEFAULT_PAGE_SIZE = 50;
