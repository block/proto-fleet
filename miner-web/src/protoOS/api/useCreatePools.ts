import { useCallback } from "react";

import { PoolConfig } from "./types";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import {
  getAuthHeader,
  useAuthContext,
  useAuthErrors,
} from "@/protoOS/contexts/AuthContext";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext/useMinerStatus";

interface CreatePoolsProps {
  accessTokenValue?: string;
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
  poolInfo: PoolConfig;
  retryOnMinerDown?: boolean;
}

const useCreatePools = () => {
  const { api } = useMinerHosting();

  const { fetchPoolsInfo } = useMinerStatus();
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
          fetchPoolsInfo({ retryOnMinerDown });
        });
    },
    [authTokens.accessToken.value, handleAuthErrors, fetchPoolsInfo, api],
  );

  return {
    createPools,
  };
};

export { useCreatePools };
