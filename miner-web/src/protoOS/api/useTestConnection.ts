import { useState } from "react";

import { TestConnection } from "./types";
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

  const testConnection = ({
    poolInfo,
    onSuccess,
    onError,
    onFinally,
  }: TestConnectionProps) => {
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
  };

  return {
    pending,
    testConnection,
  };
};

export { useTestConnection };
