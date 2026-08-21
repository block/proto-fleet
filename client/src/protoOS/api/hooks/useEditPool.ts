import { useCallback } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { usePoolsInfo } from "@/protoOS/api/hooks/usePoolsInfo";
import { poolInfoToPoolConfig } from "@/protoOS/api/poolAdapters";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store";
import { PoolInfo } from "@/shared/components/MiningPools/types";

interface EditPoolProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
  poolId: number;
  poolInfo: PoolInfo;
  retryOnMinerDown?: boolean;
}

const useEditPool = () => {
  const { api } = useMinerHosting();

  const { fetchData } = usePoolsInfo();
  const authRetry = useAuthRetry();

  const editPool = useCallback(
    async ({ poolId, poolInfo, onSuccess, onError, retryOnMinerDown }: EditPoolProps) => {
      if (!api) return;

      await authRetry({
        request: (header) => api.editPool({ id: poolId }, poolInfoToPoolConfig(poolInfo), header),
        onSuccess,
        onError,
      }).finally(() => fetchData({ retryOnMinerDown }));
    },
    [api, authRetry, fetchData],
  );

  return {
    editPool,
  };
};

export { useEditPool };
