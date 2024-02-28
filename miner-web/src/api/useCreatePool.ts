import { useCallback } from "react";

import { api } from "./api";
import { MessageResponse, PoolConfig } from "./types";

interface CreatePoolProps {
  onError?: (response: MessageResponse) => void;
  onSuccess?: (response: MessageResponse) => void;
  poolInfo: PoolConfig;
}

const useCreatePool = () => {
  const createPool = useCallback(
    async ({ poolInfo, onSuccess, onError }: CreatePoolProps) => {
      await api
        .createPool(poolInfo)
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
    createPool,
  };
};

export { useCreatePool };
