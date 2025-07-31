import { useCallback, useEffect, useState } from "react";

import { authClient, onboardingClient } from "@/protoFleet/api/clients";
import {
  UpdatePasswordRequest,
  UpdateUsernameRequest,
} from "@/protoFleet/api/generated/auth/v1/auth_pb";
import { CreateAdminLoginRequest } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";
import {
  getAuthHeader,
  useAuthContext,
} from "@/protoFleet/features/auth/contexts/AuthContext";

interface SetPasswordProps {
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: () => void;
  setPasswordRequest: CreateAdminLoginRequest;
}
interface UpdatePasswordProps {
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: () => void;
  currentPassword: UpdatePasswordRequest["currentPassword"];
  newPassword: UpdatePasswordRequest["newPassword"];
}

interface UpdateUsernameProps {
  username: UpdateUsernameRequest["username"];
  onSuccess?: () => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

const useAuth = () => {
  const { authTokens, setUsername } = useAuthContext();
  const [passwordLastUpdatedAt, setPasswordLastUpdatedAt] =
    useState<Date | null>(null);

  const setPassword = useCallback(
    async ({
      setPasswordRequest,
      onSuccess,
      onError,
      onFinally,
    }: SetPasswordProps) => {
      await onboardingClient
        .createAdminLogin(setPasswordRequest)
        .then(() => {
          setUsername(setPasswordRequest.username);
          onSuccess?.();
        })
        .catch((err) => {
          onError?.(err?.error ?? err);
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [setUsername],
  );

  const fetchLastUpdatedPasswordDate = useCallback(async () => {
    try {
      const response = await authClient.getUserAuditInfo(
        {},
        getAuthHeader(authTokens),
      );

      if (response.info?.passwordUpdatedAt) {
        const seconds = Number(response.info?.passwordUpdatedAt?.seconds);
        const date = new Date(seconds * 1000);
        setPasswordLastUpdatedAt(date);
      }
    } catch (error) {
      console.error("Error fetching last updated password date:", error);
    }
  }, [authTokens]);

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
          fetchLastUpdatedPasswordDate();
        })
        .catch((err) => {
          onError?.(err?.error ?? err);
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [authTokens, fetchLastUpdatedPasswordDate],
  );

  const updateUsername = useCallback(
    async ({
      username,
      onSuccess,
      onError,
      onFinally,
    }: UpdateUsernameProps) => {
      await authClient
        .updateUsername({ username }, getAuthHeader(authTokens))
        .then(() => {
          setUsername(username);
          onSuccess?.();
        })
        .catch((err) => {
          onError?.(err?.error ?? err);
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [authTokens, setUsername],
  );

  useEffect(() => {
    fetchLastUpdatedPasswordDate();
  }, [fetchLastUpdatedPasswordDate]);

  return {
    setPassword,
    updatePassword,
    updateUsername,
    passwordLastUpdatedAt,
  };
};

export { useAuth };
