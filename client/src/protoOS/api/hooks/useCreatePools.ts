import { useCallback } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { usePoolsInfo } from "@/protoOS/api/hooks/usePoolsInfo";
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
  const { adapter } = useMinerHosting();

  const { fetchData } = usePoolsInfo();
  const authRetry = useAuthRetry();

  const createPools = useCallback(
    async ({ poolInfo, onSuccess, onError, retryOnMinerDown }: CreatePoolsProps) => {
      if (!adapter) return;

      await authRetry({
        request: (header) => adapter.createPools(poolInfo, header),
        onSuccess,
        onError,
      }).finally(() => fetchData({ retryOnMinerDown }));
    },
    [adapter, authRetry, fetchData],
  );

  return {
    createPools,
  };
};

export { useCreatePools };
