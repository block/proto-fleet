import { useCallback } from "react";

import { authClient } from "@/protoFleet/api/clients";
import type { AuthenticateRequest } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import {
  useSetAuthLoading,
  useSetAuthTokens,
  useSetUsername,
} from "@/protoFleet/store";
import { useAuthErrors } from "@/protoFleet/store/hooks/useAuth";

interface LoginProps {
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: (accessToken: string, requiresPasswordChange: boolean) => void;
  loginRequest: AuthenticateRequest;
}

const useLogin = () => {
  const setAuthTokens = useSetAuthTokens();
  const setUsername = useSetUsername();
  const setAuthLoading = useSetAuthLoading();
  const { handleAuthErrors } = useAuthErrors();

  const login = useCallback(
    async ({ loginRequest, onSuccess, onError, onFinally }: LoginProps) => {
      await authClient
        .authenticate(loginRequest)
        .then((res) => {
          const accessTokenValue = res.token;
          const tokenExpiry = res.tokenExpiry;
          const requiresPasswordChange = res.requiresPasswordChange;
          setAuthTokens({
            accessToken: {
              value: accessTokenValue,
              expiry: new Date(Number(tokenExpiry) * 1000),
            },
          });
          setUsername(loginRequest.username);
          setAuthLoading(false);
          onSuccess?.(accessTokenValue, requiresPasswordChange);
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(err?.message ?? String(err));
            },
          });
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [setAuthTokens, setUsername, setAuthLoading, handleAuthErrors],
  );

  return login;
};

export { useLogin };
