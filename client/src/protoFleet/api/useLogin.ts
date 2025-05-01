import { useCallback } from "react";

import { authServiceClient } from "@/protoFleet/api/auth-service-client";
import type { AuthenticateRequest } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import { useAuthContext } from "@/protoFleet/contexts/AuthContext";

interface LoginProps {
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: (accessToken: string) => void;
  password: AuthenticateRequest["password"];
}

const useLogin = () => {
  const { setAuthTokens } = useAuthContext();

  const login = useCallback(
    async ({ password, onSuccess, onError, onFinally }: LoginProps) => {
      await authServiceClient
        .authenticate({ username: "admin", password })
        .then((res) => {
          const accessTokenValue = res.token;
          const tokenExpiry = res.tokenExpiry;
          setAuthTokens({
            accessToken: {
              value: accessTokenValue,
              expiry: new Date(Number(tokenExpiry) * 1000),
            },
          });
          onSuccess?.(accessTokenValue);
        })
        .catch((err) => {
          onError?.(err?.error?.message ?? err);
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [setAuthTokens],
  );

  return login;
};

export { useLogin };
