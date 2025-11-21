import { useCallback, useMemo } from "react";
import { Code, ConnectError } from "@connectrpc/connect";
import { useFleetStore } from "../useFleetStore";

// =============================================================================
// Auth State Selectors
// =============================================================================

export const useAuthTokens = () =>
  useFleetStore((state) => state.auth.authTokens);

export const useAccessToken = () =>
  useFleetStore((state) => state.auth.authTokens.accessToken);

export const useUsername = () => useFleetStore((state) => state.auth.username);

export const useAuthLoading = () =>
  useFleetStore((state) => state.auth.authLoading);

export const useTemporaryPassword = () =>
  useFleetStore((state) => state.auth.temporaryPassword);

// =============================================================================
// Auth Action Selectors
// =============================================================================

export const useSetAuthTokens = () =>
  useFleetStore((state) => state.auth.setAuthTokens);

export const useSetUsername = () =>
  useFleetStore((state) => state.auth.setUsername);

export const useSetAuthLoading = () =>
  useFleetStore((state) => state.auth.setAuthLoading);

export const useSetTemporaryPassword = () =>
  useFleetStore((state) => state.auth.setTemporaryPassword);

export const useLogout = () => useFleetStore((state) => state.auth.logout);

// =============================================================================
// Auth Utilities
// =============================================================================

/**
 * Hook that returns the authorization header for API requests
 * Uses the access token from the store
 * @returns Request params with Authorization header
 */
export const useAuthHeader = () => {
  // Select only the token value to avoid re-renders when authTokens object reference changes
  const accessTokenValue = useFleetStore(
    (state) => state.auth.authTokens.accessToken.value,
  );

  return useMemo(
    () => ({
      headers: { Authorization: `Bearer ${accessTokenValue}` },
    }),
    [accessTokenValue],
  );
};

// =============================================================================
// Auth Error Handling
// =============================================================================

interface HandleAuthErrorsProps {
  error: unknown;
  onError?: (err: unknown) => void;
  onSuccess?: (accessToken: string) => void;
}

/**
 * Hook for handling authentication errors consistently across the app
 * Currently logs out immediately on 401 errors
 * Structure supports adding refresh token logic in the future
 */
export const useAuthErrors = () => {
  const logout = useLogout();

  const handleAuthErrors = useCallback(
    ({ error, onError, onSuccess }: HandleAuthErrorsProps) => {
      if (
        error instanceof ConnectError &&
        error.code === Code.Unauthenticated
      ) {
        // TODO: Add refresh token logic here when available
        // For now, just logout immediately
        void onSuccess?.("");
        logout();
        onError?.(error);
      } else {
        onError?.(error);
      }
    },
    [logout],
  );

  return useMemo(
    () => ({
      handleAuthErrors,
    }),
    [handleAuthErrors],
  );
};
