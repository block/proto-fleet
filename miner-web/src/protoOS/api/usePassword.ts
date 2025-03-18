import { useCallback } from "react";

import { PasswordRequest } from "./types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface PasswordProps {
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: () => void;
  password: PasswordRequest["password"];
}

const usePassword = () => {
  const { api } = useMinerHosting();
  const setPassword = useCallback(
    async ({ password, onSuccess, onError, onFinally }: PasswordProps) => {
      if (!api) return;
      await api
        .setPassword({ password })
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          onError?.(err?.error?.message ?? err?.error ?? err);
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [api],
  );

  return setPassword;
};

export { usePassword };
