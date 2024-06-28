import { useCallback } from "react";

import { api } from "./api";
import { MessageResponse, PoolConfig } from "./types";

interface CreatePoolsProps {
  onError?: (response: MessageResponse) => void;
  onSuccess?: (response: MessageResponse) => void;
  poolInfo: PoolConfig;
}

const useCreatePools = () => {
  const createPools = useCallback(
    async ({ poolInfo, onSuccess, onError }: CreatePoolsProps) => {
      await api
        .createPools(poolInfo)
        .then((data) => {
          onSuccess?.(data?.data);
        })
        .catch((err) => {
          onError?.(err);
        })
    },
    []
  );

  return {
    createPools,
  };
};

export { useCreatePools };
