import { useCallback, useMemo } from "react";

import { ChangePasswordRequest, PasswordRequest } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthErrors, useAuthHeader, useSetPasswordSet } from "@/protoOS/store";

interface SetPasswordProps {
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: () => void;
  password: PasswordRequest["password"];
}

interface ChangePasswordProps {
  changePasswordRequest: ChangePasswordRequest;
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: () => void;
}

const isPasswordVerificationError = (err: unknown): boolean => {
  const message = (err as { error?: { message?: string } })?.error?.message;
  return typeof message === "string" && message.includes("Password verification error");
};

const getErrorMessage = (err: unknown, fallback = "An error occurred"): string =>
  (err as { error?: { message?: string } })?.error?.message ?? fallback;

const usePassword = () => {
  const { api } = useMinerHosting();
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();
  const setPasswordSet = useSetPasswordSet();

  const setPassword = useCallback(
    async ({ password, onSuccess, onError, onFinally }: SetPasswordProps) => {
      if (!api) return;

      const onSetSuccess = () => {
        setPasswordSet(true);
        onSuccess?.();
      };

      await api
        .setPassword({ password })
        .then(() => {
          onSetSuccess();
          onFinally?.();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
              onFinally?.();
            },
            onSuccess: () =>
              api
                .setPassword({ password })
                .then(onSetSuccess)
                .catch((retryErr) => onError?.(getErrorMessage(retryErr)))
                .finally(() => onFinally?.()),
          });
        });
    },
    [api, handleAuthErrors, setPasswordSet],
  );

  const changePassword = useCallback(
    async ({ changePasswordRequest, onSuccess, onError, onFinally }: ChangePasswordProps) => {
      if (!api) return;

      await api
        .changePassword(changePasswordRequest, authHeader)
        .then(() => {
          onSuccess?.();
          onFinally?.();
        })
        .catch((err) => {
          if (isPasswordVerificationError(err)) {
            onError?.(getErrorMessage(err));
            onFinally?.();
            return;
          }
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
              onFinally?.();
            },
            onSuccess: (accessToken) =>
              api
                .changePassword(changePasswordRequest, {
                  headers: { Authorization: `Bearer ${accessToken}` },
                })
                .then(() => onSuccess?.())
                .catch((retryErr) => onError?.(getErrorMessage(retryErr)))
                .finally(() => onFinally?.()),
          });
        });
    },
    [authHeader, api, handleAuthErrors],
  );

  return useMemo(() => ({ setPassword, changePassword }), [setPassword, changePassword]);
};

export { usePassword };
