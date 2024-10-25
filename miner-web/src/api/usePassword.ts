import { useCallback } from "react";

import { api } from "./api";
import { PasswordRequest } from "./types";

interface PasswordProps {
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: () => void;
  password: PasswordRequest["password"];
}

const usePassword = () => {
  const setPassword = useCallback(
    async ({ password, onSuccess, onError, onFinally }: PasswordProps) => {
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
    []
  );

  return {
    setPassword,
  };
};

export { usePassword };
