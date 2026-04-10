import { useCallback, useEffect, useState } from "react";

import { authClient, onboardingClient } from "@/protoFleet/api/clients";
import { UpdatePasswordRequest, UpdateUsernameRequest } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import { CreateAdminLoginRequest } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors, useSetUsername } from "@/protoFleet/store";

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
  const setUsername = useSetUsername();
  const { handleAuthErrors } = useAuthErrors();
  const [passwordLastUpdatedAt, setPasswordLastUpdatedAt] = useState<Date | null>(null);

  const setPassword = useCallback(
    async ({ setPasswordRequest, onSuccess, onError, onFinally }: SetPasswordProps) => {
      await onboardingClient
        .createAdminLogin(setPasswordRequest)
        .then(() => {
          setUsername(setPasswordRequest.username);
          onSuccess?.();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [setUsername, handleAuthErrors],
  );

  const fetchLastUpdatedPasswordDate = useCallback(async () => {
    try {
      const response = await authClient.getUserAuditInfo({});

      if (response.info?.passwordUpdatedAt && response.info.passwordUpdatedAt.seconds > 0) {
        const seconds = Number(response.info.passwordUpdatedAt.seconds);
        const date = new Date(seconds * 1000);
        setPasswordLastUpdatedAt(date);
      } else {
        setPasswordLastUpdatedAt(null);
      }
    } catch (error) {
      handleAuthErrors({
        error,
        onError: () => {
          console.error("Error fetching last updated password date:", error);
        },
      });
    }
  }, [handleAuthErrors]);

  const updatePassword = useCallback(
    async ({ currentPassword, newPassword, onSuccess, onError, onFinally }: UpdatePasswordProps) => {
      await authClient
        .updatePassword({ currentPassword, newPassword })
        .then(() => {
          onSuccess?.();
          fetchLastUpdatedPasswordDate();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [fetchLastUpdatedPasswordDate, handleAuthErrors],
  );

  const updateUsername = useCallback(
    async ({ username, onSuccess, onError, onFinally }: UpdateUsernameProps) => {
      await authClient
        .updateUsername({ username })
        .then(() => {
          setUsername(username);
          onSuccess?.();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [setUsername, handleAuthErrors],
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
