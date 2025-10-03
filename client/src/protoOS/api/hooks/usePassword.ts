import { useCallback, useMemo } from "react";

import {
  ChangePasswordRequest,
  PasswordRequest,
} from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  getAuthHeader,
  useAuthContext,
} from "@/protoOS/features/auth/contexts/AuthContext";

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

  const { authTokens } = useAuthContext();

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
      accessTokenValue,
      onSuccess,
      onError,
      onFinally,
    }: ChangePasswordProps) => {
      if (!api) return;
      await api
        .changePassword(
          changePasswordRequest,
          getAuthHeader(accessTokenValue ?? authTokens.accessToken.value),
        )
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
    [authTokens.accessToken.value, api],
  );

  return useMemo(
    () => ({ setPassword, changePassword }),
    [setPassword, changePassword],
  );
};

export { usePassword };
