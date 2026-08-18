import { useCallback } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { usePoolsInfo } from "@/protoOS/api/hooks/usePoolsInfo";
import { poolInfoToPoolConfig } from "@/protoOS/api/poolAdapters";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store";
import { PoolInfo } from "@/shared/components/MiningPools/types";

interface CreatePoolsProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
  poolInfo: PoolInfo[];
  retryOnMinerDown?: boolean;
}

const useCreatePools = () => {
  const { api } = useMinerHosting();

  const { fetchData } = usePoolsInfo();
  const authRetry = useAuthRetry();

  const createPools = useCallback(
    async ({ poolInfo, onSuccess, onError, retryOnMinerDown }: CreatePoolsProps) => {
      if (!api) return;

      await authRetry({
        request: (header) => api.createPools(poolInfo.map(poolInfoToPoolConfig), header),
        onSuccess,
        onError,
      }).finally(() => fetchData({ retryOnMinerDown }));
    },
    [api, authRetry, fetchData],
  );

  return {
    createPools,
  };
};

export { useCreatePools };
