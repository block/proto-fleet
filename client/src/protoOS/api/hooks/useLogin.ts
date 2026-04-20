import { useCallback } from "react";

import { PasswordRequest } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useSetAuthTokens } from "@/protoOS/store";
import { accessTokenExpiryTime, refreshTokenExpiryTime } from "@/shared/utils/utility";

interface LoginProps {
  onError?: (message: string) => void;
  onFinally?: () => void;
  onSuccess?: (accessToken: string, refreshToken: string) => void;
  password: PasswordRequest["password"];
}

const useLogin = () => {
  const { api } = useMinerHosting();
  const setAuthTokens = useSetAuthTokens();

  const login = useCallback(
    async ({ password, onSuccess, onError, onFinally }: LoginProps) => {
      if (!api) return;

      await api
        .login({ password }, { secure: false })
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
          onError?.(err?.error?.message ?? "An error occurred");
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [setAuthTokens, api],
  );

  return login;
};

export { useLogin };
