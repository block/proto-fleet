import { useCallback, useMemo, useState } from "react";

import { TestConnection } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

export interface TestConnectionProps {
  onError?: () => void;
  onFinally?: () => void;
  onSuccess?: () => void;
  poolInfo: TestConnection;
}

const useTestConnection = () => {
  const { api } = useMinerHosting();
  const [pending, setPending] = useState(false);

  const testConnection = useCallback(
    ({ poolInfo, onSuccess, onError, onFinally }: TestConnectionProps) => {
      if (!api) return;

      setPending(true);
      api
        .testPoolConnection(poolInfo)
        .then(() => onSuccess?.())
        .catch(() => onError?.())
        .finally(() => {
          setPending(false);
          onFinally?.();
        });
    },
    [api],
  );

  return useMemo(() => ({ pending, testConnection }), [pending, testConnection]);
};

export { useTestConnection };
