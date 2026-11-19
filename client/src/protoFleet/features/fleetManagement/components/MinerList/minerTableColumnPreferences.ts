import { minerCols, type MinerColumn } from "./constants";

export const configurableMinerColumns = [
  minerCols.model,
  minerCols.macAddress,
  minerCols.ipAddress,
  minerCols.status,
  minerCols.issues,
  minerCols.poolConfiguration,
  minerCols.workerName,
  minerCols.hashrate,
  minerCols.efficiency,
  minerCols.powerUsage,
  minerCols.temperature,
  minerCols.firmware,
  minerCols.site,
  minerCols.building,
  minerCols.rack,
  minerCols.groups,
  minerCols.assignedAccount,
] as const;

export type ConfigurableMinerColumn = (typeof configurableMinerColumns)[number];

export type MinerTableColumnPreference = {
  id: ConfigurableMinerColumn;
  visible: boolean;
};

export type MinerTableColumnPreferences = {
  columns: MinerTableColumnPreference[];
};

const STORAGE_KEY_PREFIX = "proto-fleet-miner-table-columns";

const isConfigurableMinerColumn = (value: unknown): value is ConfigurableMinerColumn =>
  configurableMinerColumns.includes(value as ConfigurableMinerColumn);

export const createDefaultMinerTableColumnPreferences = (): MinerTableColumnPreferences => ({
  columns: configurableMinerColumns.map((id) => ({ id, visible: true })),
});

export const normalizeMinerTableColumnPreferences = (
  preferences?: Partial<MinerTableColumnPreferences> | null,
): MinerTableColumnPreferences => {
  const columns: MinerTableColumnPreference[] = [];
  const seenIds = new Set<ConfigurableMinerColumn>();

  for (const column of preferences?.columns ?? []) {
    if (!column || !isConfigurableMinerColumn(column.id) || seenIds.has(column.id)) {
      continue;
    }

    seenIds.add(column.id);
    columns.push({
      id: column.id,
      visible: column.visible !== false,
    });
  }

  for (const id of configurableMinerColumns) {
    if (seenIds.has(id)) {
      continue;
    }

    columns.push({
      id,
      visible: true,
    });
  }

  return { columns };
};

export const areMinerTableColumnPreferencesDefault = (preferences: MinerTableColumnPreferences): boolean => {
  const normalizedPreferences = normalizeMinerTableColumnPreferences(preferences);

  return normalizedPreferences.columns.every(
    (column, index) => column.id === configurableMinerColumns[index] && column.visible,
  );
};

export const buildActiveMinerColumns = (preferences: MinerTableColumnPreferences): MinerColumn[] => [
  minerCols.name,
  ...normalizeMinerTableColumnPreferences(preferences)
    .columns.filter((column) => column.visible)
    .map((column) => column.id),
];

export const getMinerTableColumnPreferencesStorageKey = (username: string): string =>
  `${STORAGE_KEY_PREFIX}:${username || "anonymous"}`;
