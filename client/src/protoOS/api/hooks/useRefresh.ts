import { useCallback } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { RefreshRequest } from "@/protoOS/api/generatedApi";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import useMinerStore from "@/protoOS/store/useMinerStore";

import { accessTokenExpiryTime } from "@/shared/utils/utility";

interface RefreshProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: (accessToken: string) => void | Promise<void>;
  refreshToken: RefreshRequest["refresh_token"];
}

const useRefresh = () => {
  const { api } = useMinerHosting();
  const authTokens = useMinerStore((state) => state.auth.authTokens);
  const setAuthTokens = useMinerStore((state) => state.auth.setAuthTokens);

  const refresh = useCallback(
    async ({ refreshToken, onSuccess, onError }: RefreshProps) => {
      if (!api) return;
      await api
        .refreshToken({ refresh_token: refreshToken })
        .then((res: any) => {
          const accessTokenValue = res?.data["access_token"];
          setAuthTokens({
            ...authTokens,
            accessToken: {
              value: accessTokenValue,
              expiry: accessTokenExpiryTime(),
            },
          });
          // Catch to prevent retry errors from triggering refresh's onError (which logs out).
          return Promise.resolve(onSuccess?.(accessTokenValue)).catch((err) => {
            console.warn("Retry after token refresh failed:", err);
          });
        })
        .catch((err: any) => {
          onError?.(err);
        });
    },
    [authTokens, setAuthTokens, api],
  );

  return refresh;
};

export { useRefresh };
