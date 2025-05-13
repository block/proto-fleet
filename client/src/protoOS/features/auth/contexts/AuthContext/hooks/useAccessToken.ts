import { useCallback, useEffect, useState } from "react";

import { useAuthContext } from "./useAuthContext";
import { useRefresh } from "@/protoOS/api";

const useAccessToken = (shouldCheckAccess = true) => {
  const refresh = useRefresh();
  const { authTokens, setShowLoginModal } = useAuthContext();

  // returns undefined if access is not needed
  // returns true if access token is valid
  // returns false if refresh token is invalid
  const [hasAccess, setHasAccess] = useState<boolean | undefined>(undefined);

  const dateNow = new Date();
  const dateAccessToken = new Date(authTokens.accessToken.expiry);
  const dateRefreshToken = new Date(authTokens.refreshToken.expiry);
  const isValidAccessToken = dateAccessToken > dateNow;
  const isValidRefreshToken = dateRefreshToken > dateNow;

  const checkAccess = useCallback(() => {
    if (!shouldCheckAccess) {
      return;
    }

    if (isValidAccessToken && isValidRefreshToken) {
      setHasAccess(true);
      return;
    }

    // refresh token is expired, show login modal
    if (!isValidRefreshToken) {
      setHasAccess(false);
      setShowLoginModal(true);
      return;
    }

    // If access token has expired but refresh token is valid, refresh the access token
    if (!isValidAccessToken && isValidRefreshToken) {
      refresh({
        refreshToken: authTokens.refreshToken.value,
        onSuccess: () => {
          setHasAccess(true);
        },
        onError: () => {
          setHasAccess(false);
          setShowLoginModal(true);
        },
      });
    }
  }, [
    authTokens,
    setShowLoginModal,
    refresh,
    isValidAccessToken,
    isValidRefreshToken,
    shouldCheckAccess,
  ]);

  useEffect(() => {
    checkAccess();
  }, [checkAccess]);

  return { checkAccess, hasAccess, setHasAccess };
};

export { useAccessToken };
