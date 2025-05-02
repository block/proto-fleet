import { useCallback } from "react";

import { onboardingClient } from "@/protoFleet/api/clients";
import { CreateAdminLoginRequest } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";

interface PasswordProps {
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: () => void;
  password: CreateAdminLoginRequest["password"];
}

const usePassword = () => {
  const setPassword = useCallback(
    async ({ password, onSuccess, onError, onFinally }: PasswordProps) => {
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

  return setPassword;
};

export { usePassword };
