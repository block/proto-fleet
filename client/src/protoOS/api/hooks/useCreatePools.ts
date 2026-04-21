import { useCallback } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { PoolConfig } from "@/protoOS/api/generatedApi";

import { usePoolsInfo } from "@/protoOS/api/hooks/usePoolsInfo";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store";

interface CreatePoolsProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
  poolInfo: PoolConfig;
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
        request: (header) => api.createPools(poolInfo, header),
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
