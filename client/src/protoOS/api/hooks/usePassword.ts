import { useCallback, useMemo } from "react";

import {
  ChangePasswordRequest,
  PasswordRequest,
} from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthHeader } from "@/protoOS/store";

interface SetPasswordProps {
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: () => void;
  password: PasswordRequest["password"];
}

interface ChangePasswordProps {
  changePasswordRequest: ChangePasswordRequest;
  accessTokenValue?: string;
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: () => void;
}

const usePassword = () => {
  const { api } = useMinerHosting();

  const authHeader = useAuthHeader();

  const setPassword = useCallback(
    async ({ password, onSuccess, onError, onFinally }: SetPasswordProps) => {
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

  const changePassword = useCallback(
    async ({
      changePasswordRequest,
      accessTokenValue: _accessTokenValue,
      onSuccess,
      onError,
      onFinally,
    }: ChangePasswordProps) => {
      if (!api) return;
      await api
        .changePassword(changePasswordRequest, authHeader)
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
    [authHeader, api],
  );

  return useMemo(
    () => ({ setPassword, changePassword }),
    [setPassword, changePassword],
  );
};

export { usePassword };
