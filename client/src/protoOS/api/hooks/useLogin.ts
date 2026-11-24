import { useCallback } from "react";

import { PasswordRequest } from "@/protoOS/api/adapters/mdkV1";
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
  const { adapter } = useMinerHosting();
  const setAuthTokens = useSetAuthTokens();

  const login = useCallback(
    async ({ password, onSuccess, onError, onFinally }: LoginProps) => {
      if (!adapter) return;

      await adapter
        .login(password)
        .then(({ accessToken, refreshToken }) => {
          setAuthTokens({
            accessToken: {
              value: accessToken,
              expiry: accessTokenExpiryTime(),
            },
            refreshToken: {
              value: refreshToken,
              expiry: refreshTokenExpiryTime(),
            },
          });
          onSuccess?.(accessToken, refreshToken);
        })
        .catch((err) => {
          onError?.(err?.error?.message ?? "An error occurred");
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [setAuthTokens, adapter],
  );

  return login;
};

export { useLogin };
