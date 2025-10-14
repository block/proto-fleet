import { useCallback } from "react";

import { SimpleErrorProps } from "@/protoOS/api/apiResponseTypes";
import { PoolConfigInner } from "@/protoOS/api/generatedApi";

import { usePoolsInfo } from "@/protoOS/api/hooks/usePoolsInfo";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  getAuthHeader,
  useAuthContext,
  useAuthErrors,
} from "@/protoOS/features/auth/contexts/AuthContext";

interface EditPoolProps {
  onError?: (err: SimpleErrorProps) => void;
  onSuccess?: () => void;
  poolId: number;
  poolInfo: PoolConfigInner;
  retryOnMinerDown?: boolean;
}

const useEditPool = () => {
  const { api } = useMinerHosting();

  const { fetchData } = usePoolsInfo();
  const { authTokens } = useAuthContext();
  const { handleAuthErrors } = useAuthErrors();

  const editPool = useCallback(
    async ({
      poolId,
      poolInfo,
      onSuccess,
      onError,
      retryOnMinerDown,
    }: EditPoolProps) => {
      if (!api) return;

      await api
        .editPool(
          { id: poolId },
          poolInfo,
          getAuthHeader(authTokens.accessToken.value),
        )
        .then(() => {
          onSuccess?.();
        })
        .catch((error) => {
          handleAuthErrors({
            error,
            // @ts-ignore
            onError,
            onSuccess: () => {
              editPool({
                poolId,
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
    editPool,
  };
};

export { useEditPool };
