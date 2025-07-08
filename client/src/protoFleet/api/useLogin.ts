import { useCallback } from "react";

import { authClient } from "@/protoFleet/api/clients";
import type { AuthenticateRequest } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import { useAuthContext } from "@/protoFleet/features/auth/contexts/AuthContext";

interface LoginProps {
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: (accessToken: string) => void;
  loginRequest: AuthenticateRequest;
}

const useLogin = () => {
  const { setAuthTokens, setUsername } = useAuthContext();

  const login = useCallback(
    async ({ loginRequest, onSuccess, onError, onFinally }: LoginProps) => {
      await authClient
        .authenticate(loginRequest)
        .then((res) => {
          const accessTokenValue = res.token;
          const tokenExpiry = res.tokenExpiry;
          setAuthTokens({
            accessToken: {
              value: accessTokenValue,
              expiry: new Date(Number(tokenExpiry) * 1000),
            },
          });
          setUsername(loginRequest.username);
          onSuccess?.(accessTokenValue);
        })
        .catch((err) => {
          onError?.(err?.error?.message ?? err);
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [setAuthTokens, setUsername],
  );

  return login;
};

export { useLogin };
