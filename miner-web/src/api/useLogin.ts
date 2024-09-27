import { useCallback } from "react";

import { useAuthContext } from "common/hooks/useAuthContext";

import {
  accessTokenExpiryTime,
  refreshTokenExpiryTime,
} from "components/LoginModal/utility";

import { api } from "./api";
import { PasswordRequest } from "./types";

interface LoginProps {
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: (accessToken: string, refreshToken: string) => void;
  password: PasswordRequest["password"];
}

const useLogin = () => {
  const { setAuthTokens } = useAuthContext();

  const login = useCallback(
    async ({ password, onSuccess, onError, onFinally }: LoginProps) => {
      await api
        .login({ password })
        .then((res) => {
          const accessTokenValue = res?.data["access_token"];
          const refreshTokenValue = res?.data["refresh_token"];
          setAuthTokens({
            accessToken: {
              value: accessTokenValue,
              expiry: accessTokenExpiryTime(),
            },
            refreshToken: {
              value: refreshTokenValue,
              expiry: refreshTokenExpiryTime(),
            },
          });
          onSuccess?.(accessTokenValue, refreshTokenValue);
        })
        .catch((err) => {
          onError?.(err?.error?.message || err);
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [setAuthTokens]
  );

  return {
    login,
  };
};

export { useLogin };
