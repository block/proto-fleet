import { useCallback, useMemo, useState } from "react";

import { TestConnection } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";

export interface TestConnectionProps {
  onError?: () => void;
  onFinally?: () => void;
  onSuccess?: () => void;
  poolInfo: TestConnection;
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
        request: (params) => api.testPoolConnection(poolInfo, params),
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
