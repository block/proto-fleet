import type { AdapterRequestOptions } from "./types";
import type { PoolInfo } from "@/protoOS/store/contract";

export interface PoolConfigurationSummary {
  id: string;
  name: string;
  revision: string;
  assignedCount: number;
}
export interface PoolConfigurationReadings extends PoolConfigurationSummary {
  pools: PoolInfo[];
  error?: string;
  observedBoards?: number;
}
export interface PoolConfigurationStatisticsService {
  list(params?: AdapterRequestOptions): Promise<{ enabled: boolean; configurations: PoolConfigurationSummary[] }>;
  read(
    configurations: PoolConfigurationSummary[],
    params?: AdapterRequestOptions,
  ): Promise<PoolConfigurationReadings[]>;
}
