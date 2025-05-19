import { useCallback } from "react";

import { authClient, onboardingClient } from "@/protoFleet/api/clients";
import { UpdatePasswordRequest } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import { CreateAdminLoginRequest } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";
import {
  getAuthHeader,
  useAuthContext,
} from "@/protoFleet/features/auth/contexts/AuthContext";

interface SetPasswordProps {
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: () => void;
  password: CreateAdminLoginRequest["password"];
}
interface UpdatePasswordProps {
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: () => void;
  currentPassword: UpdatePasswordRequest["currentPassword"];
  newPassword: UpdatePasswordRequest["newPassword"];
}

const usePassword = () => {
  const { authTokens } = useAuthContext();

  const setPassword = useCallback(
    async ({ password, onSuccess, onError, onFinally }: SetPasswordProps) => {
      await onboardingClient
        .createAdminLogin({ username: "admin", password })
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
    [],
  );

  const updatePassword = useCallback(
    async ({
      currentPassword,
      newPassword,
      onSuccess,
      onError,
      onFinally,
    }: UpdatePasswordProps) => {
      await authClient
        .updatePassword(
          { currentPassword, newPassword },
          getAuthHeader(authTokens),
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
    [authTokens],
  );

  return { setPassword, updatePassword };
};

export { usePassword };
