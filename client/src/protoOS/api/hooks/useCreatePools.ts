import { useCallback } from "react";

import { SimpleErrorProps } from "@/protoOS/api/apiResponseTypes";
import { PoolConfig } from "@/protoOS/api/generatedApi";

import { usePoolsInfo } from "@/protoOS/api/hooks/usePoolsInfo";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  getAuthHeader,
  useAuthContext,
  useAuthErrors,
} from "@/protoOS/features/auth/contexts/AuthContext";

interface CreatePoolsProps {
  accessTokenValue?: string;
  onError?: (err: SimpleErrorProps) => void;
  onSuccess?: () => void;
  poolInfo: PoolConfig;
  retryOnMinerDown?: boolean;
}

const useCreatePools = () => {
  const { api } = useMinerHosting();

  const { fetchData } = usePoolsInfo();
  const { authTokens } = useAuthContext();
  const { handleAuthErrors } = useAuthErrors();

  const createPools = useCallback(
    async ({
      accessTokenValue,
      poolInfo,
      onSuccess,
      onError,
      retryOnMinerDown,
    }: CreatePoolsProps) => {
      if (!api) return;

      await api
        .createPools(
          poolInfo,
          getAuthHeader(accessTokenValue || authTokens.accessToken.value),
        )
        .then(() => {
          onSuccess?.();
        })
        .catch((error) => {
          handleAuthErrors({
            error,
            // @ts-ignore
            onError,
            onSuccess: (accessTokenValue) => {
              createPools({
                accessTokenValue,
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
    [authTokens.accessToken.value, handleAuthErrors, fetchData, api],
  );

  return {
    createPools,
  };
};

export { useCreatePools };
