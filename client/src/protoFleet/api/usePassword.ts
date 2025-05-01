import { useCallback } from "react";

import { CreateAdminLoginRequest } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";
import { onboardingServiceClient } from "@/protoFleet/api/onboarding-service-client";

interface PasswordProps {
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: () => void;
  password: CreateAdminLoginRequest["password"];
}

const usePassword = () => {
  const setPassword = useCallback(
    async ({ password, onSuccess, onError, onFinally }: PasswordProps) => {
      await onboardingServiceClient
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
