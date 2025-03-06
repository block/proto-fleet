import { useCallback } from "react";

import { ErrorProps } from "./apiResponseTypes";
import { RefreshRequest } from "./types";

import { useAuthContext } from "@/protoOS/contexts/AuthContext";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

import { accessTokenExpiryTime } from "@/shared/utils/utility";

interface RefreshProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: (accessToken: string) => void;
  refreshToken: RefreshRequest["refresh_token"];
}

const useRefresh = () => {
  const { api } = useMinerHosting();
  const { authTokens, setAuthTokens } = useAuthContext();

  const refresh = useCallback(
    async ({ refreshToken, onSuccess, onError }: RefreshProps) => {
      if (!api) return;
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
    [authTokens, setAuthTokens, api],
  );

  return {
    refresh,
  };
};

export { useRefresh };
