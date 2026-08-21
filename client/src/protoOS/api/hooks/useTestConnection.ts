import { useCallback, useMemo, useState } from "react";

import { poolInfoToTestConnection } from "@/protoOS/api/poolAdapters";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { PoolInfo } from "@/shared/components/MiningPools/types";

export interface TestConnectionProps {
  onError?: () => void;
  onFinally?: () => void;
  onSuccess?: () => void;
  poolInfo: PoolInfo;
}

const useTestConnection = () => {
  const { api } = useMinerHosting();
  const authRetry = useAuthRetry();
  const [pending, setPending] = useState(false);

  const testConnection = useCallback(
    ({ poolInfo, onSuccess, onError, onFinally }: TestConnectionProps) => {
      if (!api) return;

      setPending(true);
      authRetry({
        request: (params) => api.testPoolConnection(poolInfoToTestConnection(poolInfo), params),
        onSuccess: () => onSuccess?.(),
        onError: () => onError?.(),
      }).finally(() => {
        setPending(false);
        onFinally?.();
      });
    },
    [api, authRetry],
  );

  return useMemo(() => ({ pending, testConnection }), [pending, testConnection]);
};

export { useTestConnection };
