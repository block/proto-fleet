import { useCallback } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { RefreshRequest } from "@/protoOS/api/generatedApi";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useMinerStore, useSetAuthTokens } from "@/protoOS/store";

import { accessTokenExpiryTime } from "@/shared/utils/utility";

interface RefreshProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: (accessToken: string) => void | Promise<void>;
  refreshToken: RefreshRequest["refresh_token"];
}

const useRefresh = () => {
  const { api } = useMinerHosting();
  const setAuthTokens = useSetAuthTokens();

  const refresh = useCallback(
    async ({ refreshToken, onSuccess, onError }: RefreshProps) => {
      if (!api) return;
      await api
        .refreshToken({ refresh_token: refreshToken }, { secure: false })
        .then((res: any) => {
          const accessTokenValue = res?.data["access_token"];
          const authTokens = useMinerStore.getState().auth.authTokens;
          // Drop stale responses: if the store's refresh token changed while
          // this request was in flight (logout, password rotation, login as a
          // different user), writing A's access token on top of B's refresh
          // token mixes sessions and breaks subsequent calls. Surface through
          // onError (non-401 so no logout) so the shared refreshPromise in
          // useAuthErrors settles — otherwise it would hang forever and every
          // later 401 handler would block on it.
          if (authTokens.refreshToken?.value !== refreshToken) {
            onError?.({
              status: 0,
              error: { message: "refresh response dropped: session changed mid-flight" },
            });
            return;
          }
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
    [setAuthTokens, api],
  );

  return refresh;
};

export { useRefresh };
