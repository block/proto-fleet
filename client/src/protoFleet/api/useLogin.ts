import { useCallback } from "react";

import { authClient } from "@/protoFleet/api/clients";
import type { AuthenticateRequest } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import {
  useSetAuthLoading,
  useSetIsAuthenticated,
  useSetRole,
  useSetSessionExpiry,
  useSetUsername,
} from "@/protoFleet/store";
import { useAuthErrors } from "@/protoFleet/store/hooks/useAuth";

interface LoginProps {
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: (requiresPasswordChange: boolean) => void;
  loginRequest: AuthenticateRequest;
  /**
   * When true, prevents automatic logout on authentication failure.
   * Use this for re-authentication flows (e.g., password change verification)
   * where a failed attempt should show an error, not log the user out.
   */
  skipLogoutOnError?: boolean;
}

const useLogin = () => {
  const setSessionExpiry = useSetSessionExpiry();
  const setIsAuthenticated = useSetIsAuthenticated();
  const setUsername = useSetUsername();
  const setRole = useSetRole();
  const setAuthLoading = useSetAuthLoading();
  const { handleAuthErrors } = useAuthErrors();

  const login = useCallback(
    async ({ loginRequest, onSuccess, onError, onFinally, skipLogoutOnError }: LoginProps) => {
      await authClient
        .authenticate(loginRequest)
        .then((res) => {
          const sessionExpiry = res.sessionExpiry;
          const userInfo = res.userInfo;

          if (!userInfo) {
            throw new Error("User info missing from authentication response");
          }

          // Session cookie is automatically stored by browser
          // We just track the expiry and user info in state
          setSessionExpiry(new Date(Number(sessionExpiry) * 1000));
          setIsAuthenticated(true);
          setUsername(userInfo.username);
          setRole(userInfo.role);
          setAuthLoading(false);
          onSuccess?.(userInfo.requiresPasswordChange);
        })
        .catch((err) => {
          if (skipLogoutOnError) {
            onError?.(getErrorMessage(err));
            return;
          }

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
    [setSessionExpiry, setIsAuthenticated, setUsername, setRole, setAuthLoading, handleAuthErrors],
  );

  return login;
};

export { useLogin };
