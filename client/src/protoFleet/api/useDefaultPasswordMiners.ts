import useFleet from "./useFleet";
import {
  MinerListFilter,
  MinerStateSnapshot,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

type UseDefaultPasswordMinersOptions = {
  pageSize?: number;
  filter?: MinerListFilter;
  enabled?: boolean;
};

type UseDefaultPasswordMinersReturn = {
  minerIds: string[];
  miners: Record<string, MinerStateSnapshot>;
  totalMiners: number;
  hasMore: boolean;
  isLoading: boolean;
  hasInitialLoadCompleted: boolean;
  loadMore: () => void;
  refetch: () => void;
  availableModels: string[];
};

const useDefaultPasswordMiners = (options: UseDefaultPasswordMinersOptions = {}): UseDefaultPasswordMinersReturn => {
  const { pageSize = 100, filter, enabled = true } = options;

  return useFleet({
    enabled,
    pageSize,
    filter,
    pairingStatuses: [PairingStatus.DEFAULT_PASSWORD],
  }) as UseDefaultPasswordMinersReturn;
};

export default useDefaultPasswordMiners;
