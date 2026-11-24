import { useCallback } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { usePoolsInfo } from "@/protoOS/api/hooks/usePoolsInfo";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store";
import { PoolInfo } from "@/shared/types/pool";

interface EditPoolProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
  poolId: number;
  poolInfo: PoolInfo;
  retryOnMinerDown?: boolean;
}

const useEditPool = () => {
  const { adapter } = useMinerHosting();

  const { fetchData } = usePoolsInfo();
  const authRetry = useAuthRetry();

  const editPool = useCallback(
    async ({ poolId, poolInfo, onSuccess, onError, retryOnMinerDown }: EditPoolProps) => {
      if (!adapter) return;

      await authRetry({
        request: (header) => adapter.editPool(poolId, poolInfo, header),
        onSuccess,
        onError,
      }).finally(() => fetchData({ retryOnMinerDown }));
    },
    [adapter, authRetry, fetchData],
  );

  return {
    editPool,
  };
};

export { useEditPool };
