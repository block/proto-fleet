import { useCallback, useMemo } from "react";

import { ChangePasswordRequest, PasswordRequest } from "@/protoOS/api/adapters/mdkV1";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry, useSetDefaultPasswordActive, useSetPasswordSet } from "@/protoOS/store";

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
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const setPasswordSet = useSetPasswordSet();
  const setDefaultPasswordActive = useSetDefaultPasswordActive();

  const setPassword = useCallback(
    async ({ password, onSuccess, onError, onFinally }: SetPasswordProps) => {
      if (!adapter) return;

      await authRetry({
        request: () => adapter.setPassword(password, { secure: false }),
        onSuccess: () => {
          setPasswordSet(true);
          setDefaultPasswordActive(false);
          onSuccess?.();
        },
        onError: (err) => onError?.(getErrorMessage(err)),
      }).finally(() => onFinally?.());
    },
    [adapter, authRetry, setPasswordSet, setDefaultPasswordActive],
  );

  const changePassword = useCallback(
    async ({ changePasswordRequest, onSuccess, onError, onFinally }: ChangePasswordProps) => {
      if (!adapter) return;

      await authRetry({
        request: (header) => adapter.changePassword(changePasswordRequest, header),
        onSuccess,
        onError: (err) => onError?.(getErrorMessage(err)),
        shouldRetry: (err) => !isPasswordVerificationError(err),
      }).finally(() => onFinally?.());
    },
    [adapter, authRetry],
  );

  return useMemo(() => ({ setPassword, changePassword }), [setPassword, changePassword]);
};

export { usePassword };
