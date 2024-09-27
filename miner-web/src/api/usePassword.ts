import { useCallback } from "react";

import { api } from "./api";
import { PasswordRequest } from "./types";

interface PasswordProps {
  onError?: (message: string) => void;
  onSuccess?: () => void;
  password: PasswordRequest["password"];
}

const usePassword = () => {
  const setPassword = useCallback(
    async ({ password, onSuccess, onError }: PasswordProps) => {
      await api
        .setPassword({ password })
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          onError?.(err?.error?.message || err);
        });
    },
    []
  );

  return {
    setPassword,
  };
};

export { usePassword };
