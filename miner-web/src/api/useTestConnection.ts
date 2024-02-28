import { useState } from "react";

import { api } from "./api";
import { TestConnection } from "./types";

export interface TestConnectionProps {
  onError?: () => void;
  onFinally?: () => void;
  onSuccess?: () => void;
  poolInfo: TestConnection;
}

const useTestConnection = () => {
  const [pending, setPending] = useState(false);

  const testConnection = ({
    poolInfo,
    onSuccess,
    onError,
    onFinally,
  }: TestConnectionProps) => {
    setPending(true);
    api
      .testConnection(poolInfo)
      .then(({ data }) => {
        const message = data?.message || "";
        const passed = /connection test passed/.test(message.toLowerCase());
        if (passed) {
          onSuccess?.();
        } else {
          onError?.();
        }
      })
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
