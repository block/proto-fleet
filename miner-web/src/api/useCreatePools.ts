import { useCallback } from "react";

import { api } from "./api";
import { PoolConfig } from "./types";

interface CreatePoolsProps {
  onError?: (message: string) => void;
  onSuccess?: () => void;
  poolInfo: PoolConfig;
}

const useCreatePools = () => {
  const createPools = useCallback(
    async ({ poolInfo, onSuccess, onError }: CreatePoolsProps) => {
      await api
        .createPools(poolInfo)
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          onError?.(err?.message || err);
        })
    },
    []
  );

  return {
    createPools,
  };
};

export { useCreatePools };
