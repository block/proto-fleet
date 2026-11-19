import type { PoolConfigurationMode } from "../slices/poolsSlice";
import { beginStoreWrite } from "../storeWriteGuard";
import useMinerStore from "../useMinerStore";
import type { PoolConfigurationStatisticsService } from "@/protoOS/api/adapters/poolStatistics";

export const usePoolConfigurationMode = (service?: PoolConfigurationStatisticsService | null) =>
  useMinerStore((state) => {
    const mode = state.pools.configurationMode;
    return mode && mode.service === service ? mode.value : undefined;
  });

export const beginPoolConfigurationModeRead = (service: PoolConfigurationStatisticsService, signal: AbortSignal) => {
  const accept = beginStoreWrite("pools:configuration-mode");
  return (mode: PoolConfigurationMode) => {
    if (signal.aborted) return;
    const pools = useMinerStore.getState().pools;
    if (mode === "unavailable" && pools.configurationMode?.service === service) return;
    if (accept()) pools.setConfigurationMode(service, mode);
  };
};
