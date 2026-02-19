import { useCallback, useEffect, useMemo, useState } from "react";
import { matchRoutes, useLocation } from "react-router-dom";
import useMinerStore from "../useMinerStore";
import { useRefresh } from "@/protoOS/api";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { CustomRouteObject, routerConfig } from "@/protoOS/router";

// =============================================================================
// Auth State Selectors
// =============================================================================

export const useAuthTokens = () => useMinerStore((state) => state.auth.authTokens);
export const useRefreshToken = () => useMinerStore((state) => state.auth.authTokens.refreshToken);
export const useAuthLoading = () => useMinerStore((state) => state.auth.loading);

// =============================================================================
// Auth Action Selectors
// =============================================================================

export const useSetAuthTokens = () => useMinerStore((state) => state.auth.setAuthTokens);
export const useSetAuthLoading = () => useMinerStore((state) => state.auth.setLoading);
export const useLogout = () => useMinerStore((state) => state.auth.logout);

// =============================================================================
// Auth Utilities
// =============================================================================

/**
 * Hook that returns the authorization header for API requests
 * Uses the access token from the store
 * @returns Request params with Authorization header
 */
export const useAuthHeader = () => {
  const authTokens = useAuthTokens();

  return useMemo(
    () => ({
      headers: {
        Authorization: `Bearer ${authTokens.accessToken?.value || ""}`,
      },
    }),
    [authTokens.accessToken?.value],
  );
};

// =============================================================================
// Auth Error Handling
// =============================================================================

interface HandleAuthErrorsProps {
  error: ErrorProps;
  onError?: (err: ErrorProps) => void;
  onSuccess?: (accessToken: string) => void | Promise<void>;
}

export const useAuthErrors = () => {
  const authTokens = useAuthTokens();
  const logout = useLogout();
  const setShowLoginModal = useMinerStore((state) => state.ui.setShowLoginModal);
  const refresh = useRefresh();

  const handleAuthErrors = useCallback(
    ({ error, onError, onSuccess }: HandleAuthErrorsProps) => {
      if (error?.status === 401) {
        return refresh({
          refreshToken: authTokens.refreshToken?.value || "",
          onSuccess,
          onError: (refreshError) => {
            if (refreshError?.status === 401) {
              logout();
              setShowLoginModal(true);
            }
            onError?.(error);
          },
        });
      }
      onError?.(error);
    },
    [authTokens.refreshToken?.value, refresh, logout, setShowLoginModal],
  );

  return useMemo(
    () => ({
      handleAuthErrors,
    }),
    [handleAuthErrors],
  );
};

// =============================================================================
// Access Token Management
// =============================================================================

const getRouteAuthRequirement = (path: string, defaultValue = true) => {
  const matchedRoutes = matchRoutes(routerConfig, path);
  if (!matchedRoutes) return defaultValue;
  for (let i = matchedRoutes.length - 1; i >= 0; i--) {
    const match = matchedRoutes[i];
    const route = match.route as CustomRouteObject;
    const requiresAuth = route.requiresAuth;
    if (typeof requiresAuth === "boolean") {
      return requiresAuth;
    }
  }
  return defaultValue;
};

export const useAccessToken = (shouldCheckAccess: boolean = true) => {
  const refresh = useRefresh();
  const authTokens = useAuthTokens();
  const setShowLoginModal = useMinerStore((state) => state.ui.setShowLoginModal);
  const logout = useLogout();
  const pausedAuthAction = useMinerStore((state) => state.ui.pausedAuthAction);

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
      setShowLoginModal(routeRequiresAuth || pausedAuthAction !== null);
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
          setShowLoginModal(routeRequiresAuth || pausedAuthAction !== null);
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
    routeRequiresAuth,
    pausedAuthAction,
    logout,
  ]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    checkAccess();
  }, [checkAccess]);

  return { checkAccess, hasAccess, setHasAccess, routeRequiresAuth };
};
