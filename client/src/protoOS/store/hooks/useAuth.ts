import { useCallback, useEffect, useMemo, useState } from "react";
import { useLocation } from "react-router-dom";
import useMinerStore from "../useMinerStore";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { isDefaultPasswordActiveError } from "@/protoOS/api/defaultPasswordContract";
import { useRefresh } from "@/protoOS/api/hooks/useRefresh";
import { isAuthRequiredPath } from "@/protoOS/routeAuth";

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

// Shared in-flight refresh: parallel 401s from concurrent polls + writes all
// await the same /auth/refresh call. A boolean short-circuit would hand back
// onError(originalError) to every late arrival, killing useAuthRetry's retry
// on any write unlucky enough to 401 while another hook's refresh is running.
// Resolves to the new access token on success, null on failure.
let refreshPromise: Promise<string | null> | null = null;

export const __resetRefreshInFlightForTest = () => {
  refreshPromise = null;
};

export const useAuthErrors = () => {
  const logout = useLogout();
  const setShowLoginModal = useMinerStore((state) => state.ui.setShowLoginModal);
  const setDefaultPasswordActive = useMinerStore((state) => state.minerStatus.setDefaultPasswordActive);
  const refresh = useRefresh();

  const handleAuthErrors = useCallback(
    ({ error, onError, onSuccess }: HandleAuthErrorsProps) => {
      // 403 with DEFAULT_PASSWORD_ACTIVE means the device still has its factory
      // password. Surface this in the store so the UI can prompt a password change.
      if (isDefaultPasswordActiveError(error)) {
        setDefaultPasswordActive(true);
        onError?.(error);
        return;
      }

      if (error?.status === 401) {
        if (!refreshPromise) {
          refreshPromise = new Promise<string | null>((resolve) => {
            void refresh({
              refreshToken: useMinerStore.getState().auth.authTokens.refreshToken?.value || "",
              onSuccess: (accessToken) => {
                resolve(accessToken);
              },
              onError: (refreshError) => {
                if (refreshError?.status === 401) {
                  logout();
                  setShowLoginModal(true);
                }
                resolve(null);
              },
            });
          }).finally(() => {
            refreshPromise = null;
          });
        }

        return refreshPromise.then((newToken) => {
          if (newToken !== null) {
            return onSuccess?.(newToken);
          }
          onError?.(error);
        });
      }
      onError?.(error);
    },
    [refresh, logout, setShowLoginModal, setDefaultPasswordActive],
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

export const useAccessToken = (shouldCheckAccess: boolean = true) => {
  const refresh = useRefresh();
  const authTokens = useAuthTokens();
  const setShowLoginModal = useMinerStore((state) => state.ui.setShowLoginModal);
  const logout = useLogout();
  const pausedAuthAction = useMinerStore((state) => state.ui.pausedAuthAction);
  const passwordSet = useMinerStore((state) => state.minerStatus.passwordSet);

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
  const routeRequiresAuth = useMemo(() => isAuthRequiredPath(location.pathname), [location.pathname]);
  // Only surface the login modal when the device actually has credentials to
  // log into. Before onboarding completes (passwordSet === false) the user
  // needs the onboarding flow, and while status is still loading
  // (passwordSet === undefined) the App-level redirect may not have run yet —
  // in both cases showing a modal traps the user with no way out.
  const canShowLoginModal = passwordSet === true;

  const checkAccess = useCallback(() => {
    if (!shouldCheckAccess) {
      return;
    }

    if (isValidAccessToken && isValidRefreshToken) {
      setHasAccess(true);
      setShowLoginModal(false);
      return;
    }

    const shouldShowModal = canShowLoginModal && (routeRequiresAuth || pausedAuthAction !== null);

    // refresh token is expired, show login modal
    if (!isValidRefreshToken) {
      logout();
      setHasAccess(false);
      setShowLoginModal(shouldShowModal);
      return;
    }

    // If access token has expired but refresh token is valid, refresh the access token
    if (!isValidAccessToken && isValidRefreshToken) {
      refresh({
        refreshToken: authTokens.refreshToken.value,
        onSuccess: () => {
          setHasAccess(true);
          setShowLoginModal(false);
        },
        onError: () => {
          logout();
          setHasAccess(false);
          setShowLoginModal(shouldShowModal);
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
    canShowLoginModal,
    logout,
  ]);

  useEffect(() => {
    checkAccess();
  }, [checkAccess]);

  return { checkAccess, hasAccess, setHasAccess, routeRequiresAuth };
};
