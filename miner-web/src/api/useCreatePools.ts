import { useCallback, useContext } from "react";

import { api, ApiContext } from "./api";
import { PoolConfig } from "./types";

interface CreatePoolsProps {
  onError?: (message: string) => void;
  onSuccess?: () => void;
  poolInfo: PoolConfig;
  retryOnMinerDown?: boolean;
}

const useCreatePools = () => {
  const { fetchPoolsInfo } = useContext(ApiContext);

  const createPools = useCallback(
    async ({ poolInfo, onSuccess, onError, retryOnMinerDown }: CreatePoolsProps) => {
      await api
        .createPools(poolInfo)
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          onError?.(err?.message || err);
        })
        .finally(() => {
          fetchPoolsInfo({ retryOnMinerDown });
        });
    },
    [fetchPoolsInfo]
  );

  return {
    createPools,
  };
};

export { useCreatePools };
