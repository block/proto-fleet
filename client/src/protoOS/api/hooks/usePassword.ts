import { useCallback, useMemo } from "react";

import {
  ChangePasswordRequest,
  PasswordRequest,
} from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  useAuthErrors,
  useAuthHeader,
  useSetSystemStatus,
} from "@/protoOS/store";

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

const usePassword = () => {
  const { api } = useMinerHosting();
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();
  const setSystemStatus = useSetSystemStatus();

  const setPassword = useCallback(
    async ({ password, onSuccess, onError, onFinally }: SetPasswordProps) => {
      if (!api) return;

      const performSetPassword = async () => {
        await api
          .setPassword({ password })
          .then(() => {
            // Update store to reflect that password is now set
            setSystemStatus({
              passwordSet: true,
            });
            onSuccess?.();
          })
          .catch((err) => {
            handleAuthErrors({
              error: err,
              onError: () => {
                onError?.(
                  err?.error?.message ?? err?.message ?? "An error occurred",
                );
              },
              onSuccess: () => {
                performSetPassword();
              },
            });
          })
          .finally(() => {
            onFinally?.();
          });
      };

      await performSetPassword();
    },
    [api, handleAuthErrors, setSystemStatus],
  );

  const changePassword = useCallback(
    async ({
      changePasswordRequest,
      onSuccess,
      onError,
      onFinally,
    }: ChangePasswordProps) => {
      if (!api) return;

      const performChangePassword = async () => {
        await api
          .changePassword(changePasswordRequest, authHeader)
          .then(() => {
            onSuccess?.();
          })
          .catch((err) => {
            handleAuthErrors({
              error: err,
              onError: () => {
                onError?.(
                  err?.error?.message ?? err?.message ?? "An error occurred",
                );
              },
              onSuccess: () => {
                performChangePassword();
              },
            });
          })
          .finally(() => {
            onFinally?.();
          });
      };

      await performChangePassword();
    },
    [authHeader, api, handleAuthErrors],
  );

  return useMemo(
    () => ({ setPassword, changePassword }),
    [setPassword, changePassword],
  );
};

export { usePassword };
