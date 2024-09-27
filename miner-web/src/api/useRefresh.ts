import { useCallback } from "react";

import { useAuthContext } from "common/hooks/useAuthContext";

import { accessTokenExpiryTime } from "components/LoginModal/utility";

import { api } from "./api";
import { ErrorProps } from "./apiResponseTypes";
import { RefreshRequest } from "./types";

interface RefreshProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: (accessToken: string) => void;
  refreshToken: RefreshRequest["refresh_token"];
}

const useRefresh = () => {
  const { authTokens, setAuthTokens } = useAuthContext();

  const refresh = useCallback(
    async ({ refreshToken, onSuccess, onError }: RefreshProps) => {
      await api
        .v1AuthRefreshCreate({ refresh_token: refreshToken })
        .then((res) => {
          const accessTokenValue = res?.data["access_token"];
          setAuthTokens({
            ...authTokens,
            accessToken: {
              value: accessTokenValue,
              expiry: accessTokenExpiryTime(),
            },
          });
          onSuccess?.(accessTokenValue);
        })
        .catch((err) => {
          onError?.(err);
        });
    },
    [authTokens, setAuthTokens]
  );

  return {
    refresh,
  };
};

export { useRefresh };
