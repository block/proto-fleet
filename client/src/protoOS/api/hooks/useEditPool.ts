import { useCallback } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { PoolConfigInner } from "@/protoOS/api/generatedApi";

import { usePoolsInfo } from "@/protoOS/api/hooks/usePoolsInfo";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store";

interface EditPoolProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
  poolId: number;
  poolInfo: PoolConfigInner;
  retryOnMinerDown?: boolean;
}

const useEditPool = () => {
  const { api } = useMinerHosting();

  const { fetchData } = usePoolsInfo();
  const authRetry = useAuthRetry();

  const editPool = useCallback(
    async ({ poolId, poolInfo, onSuccess, onError, retryOnMinerDown }: EditPoolProps) => {
      if (!api) return;

      await authRetry({
        request: (header) => api.editPool({ id: poolId }, poolInfo, header),
        onSuccess,
        onError,
      }).finally(() => fetchData({ retryOnMinerDown }));
    },
    [api, authRetry, fetchData],
  );

  return {
    editPool,
  };
};

export { useEditPool };
