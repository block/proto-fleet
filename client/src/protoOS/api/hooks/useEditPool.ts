import { useCallback } from "react";

import { SimpleErrorProps } from "@/protoOS/api/apiResponseTypes";
import { PoolConfigInner } from "@/protoOS/api/generatedApi";

import { usePoolsInfo } from "@/protoOS/api/hooks/usePoolsInfo";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthErrors, useAuthHeader } from "@/protoOS/store";

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
  const authHeader = useAuthHeader();
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

      const performEdit = async () => {
        await api
          .editPool({ id: poolId }, poolInfo, authHeader)
          .then(() => {
            onSuccess?.();
          })
          .catch((error) => {
            handleAuthErrors({
              error,
              // @ts-ignore
              onError,
              onSuccess: () => {
                performEdit();
              },
            });
          })
          .finally(() => {
            fetchData({ retryOnMinerDown });
          });
      };

      await performEdit();
    },
    [authHeader, handleAuthErrors, fetchData, api],
  );

  return {
    editPool,
  };
};

export { useEditPool };
