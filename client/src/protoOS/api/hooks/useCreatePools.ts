import { useCallback } from "react";

import { SimpleErrorProps } from "@/protoOS/api/apiResponseTypes";
import { PoolConfig } from "@/protoOS/api/generatedApi";

import { usePoolsInfo } from "@/protoOS/api/hooks/usePoolsInfo";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthErrors, useAuthHeader } from "@/protoOS/store";

interface CreatePoolsProps {
  onError?: (err: SimpleErrorProps) => void;
  onSuccess?: () => void;
  poolInfo: PoolConfig;
  retryOnMinerDown?: boolean;
}

const useCreatePools = () => {
  const { api } = useMinerHosting();

  const { fetchData } = usePoolsInfo();
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();

  const createPools = useCallback(
    async ({
      poolInfo,
      onSuccess,
      onError,
      retryOnMinerDown,
    }: CreatePoolsProps) => {
      if (!api) return;

      await api
        .createPools(poolInfo, authHeader)
        .then(() => {
          onSuccess?.();
        })
        .catch((error) => {
          handleAuthErrors({
            error,
            // @ts-ignore
            onError,
            onSuccess: () => {
              createPools({
                poolInfo,
                onSuccess,
                onError,
                retryOnMinerDown,
              });
            },
          });
        })
        .finally(() => {
          fetchData({ retryOnMinerDown });
        });
    },
    [authHeader, handleAuthErrors, fetchData, api],
  );

  return {
    createPools,
  };
};

export { useCreatePools };
