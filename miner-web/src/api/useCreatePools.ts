import { useCallback } from "react";

import { ErrorProps } from "apiResponseTypes";

import { useApiContext } from "common/hooks/useApiContext";
import { useAuthContext } from "common/hooks/useAuthContext";
import { useAuthErrors } from "common/hooks/useAuthErrors";

import { api } from "./api";
import { getAuthHeader } from "./constants";
import { PoolConfig } from "./types";

interface CreatePoolsProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
  poolInfo: PoolConfig;
  retryOnMinerDown?: boolean;
}

const useCreatePools = () => {
  const { fetchPoolsInfo } = useApiContext();
  const { authTokens } = useAuthContext();
  const { handleAuthErrors } = useAuthErrors();

  const createPools = useCallback(
    async ({
      poolInfo,
      onSuccess,
      onError,
      retryOnMinerDown,
    }: CreatePoolsProps) => {
      await api
        .createPools(poolInfo, getAuthHeader(authTokens.accessToken.value))
        .then(() => {
          onSuccess?.();
        })
        .catch((error) => {
          handleAuthErrors({
            error,
            onError,
            onSuccess: () => {
              createPools({ poolInfo, onSuccess, onError, retryOnMinerDown });
            },
          });
        })
        .finally(() => {
          fetchPoolsInfo({ retryOnMinerDown });
        });
    },
    [authTokens.accessToken.value, handleAuthErrors, fetchPoolsInfo]
  );

  return {
    createPools,
  };
};

export { useCreatePools };
