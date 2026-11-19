import type { AdapterRequestOptions } from "./types";
import type { PoolInfo } from "@/protoOS/store/contract";

export const isActivePool = (pool: PoolInfo) => pool.status === "Active" || pool.status === "Alive";

export interface PoolConfigurationSummary {
  id: string;
  name: string;
  revision: string;
  assignedCount: number;
}
export interface PoolConfigurationReadings extends PoolConfigurationSummary {
  pools: PoolInfo[];
  stale?: true;
  error?: string;
  observedBoards?: number;
}
export interface PoolConfigurationStatisticsService {
  getMiningGroupsEnabled(params?: AdapterRequestOptions): Promise<boolean>;
  list(params?: AdapterRequestOptions): Promise<{ enabled: boolean; configurations: PoolConfigurationSummary[] }>;
  read(
    configurations: PoolConfigurationSummary[],
    params?: AdapterRequestOptions & { includeLastRecordedShares?: boolean },
  ): Promise<PoolConfigurationReadings[]>;
}
