import { useCallback, useEffect, useMemo, useState } from "react";

import { useAuthContext } from "./useAuthContext";
import { useRefresh } from "@/protoOS/api";

import { matchRoutes, useLocation } from "react-router-dom";
import { routerConfig } from "@/protoOS/router";

const getRouteAuthRequirement = (path: string, defaultValue = true) => {
  const matchedRoutes = matchRoutes(routerConfig, path);
  if (!matchedRoutes) return defaultValue;
  for (let i = matchedRoutes.length - 1; i >= 0; i--) {
    const match = matchedRoutes[i];
    const requiresAuth = match.route.requiresAuth;
    if (typeof requiresAuth === "boolean") {
      return requiresAuth;
    }
  }
  return defaultValue;
};

const useAccessToken = (shouldCheckAccess = true, requireLogin = true) => {
  const refresh = useRefresh();
  const { authTokens, setShowLoginModal, logout } = useAuthContext();

  // returns undefined if access is not needed
  // returns true if access token is valid
  // returns false if refresh token is invalid
  const [hasAccess, setHasAccess] = useState<boolean | undefined>(undefined);

  const dateNow = new Date();
  const dateAccessToken = new Date(authTokens.accessToken.expiry);
  const dateRefreshToken = new Date(authTokens.refreshToken.expiry);
  const isValidAccessToken = dateAccessToken > dateNow;
  const isValidRefreshToken = dateRefreshToken > dateNow;
  const location = useLocation();
  const routeRequiresAuth = useMemo(() => {
    return getRouteAuthRequirement(location.pathname, false);
  }, [location.pathname]);

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
      logout();
      setHasAccess(false);
      setShowLoginModal(routeRequiresAuth);
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
          logout();
          setHasAccess(false);
          setShowLoginModal(routeRequiresAuth);
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
    requireLogin,
    routeRequiresAuth,
    logout,
  ]);

  useEffect(() => {
    checkAccess();
  }, [checkAccess]);

  return { checkAccess, hasAccess, setHasAccess, routeRequiresAuth };
};

export { useAccessToken };
