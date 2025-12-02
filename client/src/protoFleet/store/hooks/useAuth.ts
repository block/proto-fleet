import { useCallback, useMemo } from "react";
import { Code, ConnectError } from "@connectrpc/connect";
import { useFleetStore } from "../useFleetStore";

// =============================================================================
// Auth State Selectors
// =============================================================================

export const useSessionExpiry = () => useFleetStore((state) => state.auth.sessionExpiry);

export const useIsAuthenticated = () => useFleetStore((state) => state.auth.isAuthenticated);

export const useUsername = () => useFleetStore((state) => state.auth.username);

export const useRole = () => useFleetStore((state) => state.auth.role);

export const useAuthLoading = () => useFleetStore((state) => state.auth.authLoading);

export const useTemporaryPassword = () => useFleetStore((state) => state.auth.temporaryPassword);

// =============================================================================
// Auth Action Selectors
// =============================================================================

export const useSetSessionExpiry = () => useFleetStore((state) => state.auth.setSessionExpiry);

export const useSetIsAuthenticated = () => useFleetStore((state) => state.auth.setIsAuthenticated);

export const useSetUsername = () => useFleetStore((state) => state.auth.setUsername);

export const useSetRole = () => useFleetStore((state) => state.auth.setRole);

export const useSetAuthLoading = () => useFleetStore((state) => state.auth.setAuthLoading);

export const useSetTemporaryPassword = () => useFleetStore((state) => state.auth.setTemporaryPassword);

export const useLogout = () => useFleetStore((state) => state.auth.logout);

// =============================================================================
// Auth Error Handling
// =============================================================================

interface HandleAuthErrorsProps {
  error: unknown;
  onError?: (err: unknown) => void;
}

/**
 * Hook for handling authentication errors consistently across the app
 * Logs out immediately on 401 errors since session is invalid
 */
export const useAuthErrors = () => {
  const logout = useLogout();

  const handleAuthErrors = useCallback(
    ({ error, onError }: HandleAuthErrorsProps) => {
      if (error instanceof ConnectError && error.code === Code.Unauthenticated) {
        // Session is invalid or expired - logout
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
