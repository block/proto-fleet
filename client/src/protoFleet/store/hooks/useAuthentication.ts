import { useCallback, useEffect, useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { useAuthLoading, useIsAuthenticated as useIsAuthenticatedState, useSessionExpiry } from "./useAuth";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";

// =============================================================================
// Auth Access Hook
// =============================================================================

const REDIRECT_DELAY = 600;

/**
 * Hook for checking authentication status and redirecting to login if needed.
 * Uses session-based authentication with HTTP-only cookies.
 */
export const useCheckAuthentication = (shouldCheckAccess = true) => {
  const isAuthenticated = useIsAuthenticatedState();
  const sessionExpiry = useSessionExpiry();
  const loading = useAuthLoading();
  const navigate = useNavigate();

  // Check if session is valid (authenticated and not expired)
  const isSessionValid = useMemo(() => {
    if (!isAuthenticated || !sessionExpiry) {
      return false;
    }
    return sessionExpiry > new Date();
  }, [isAuthenticated, sessionExpiry]);

  // Derive hasAccess directly from session validity
  // returns undefined if access check is disabled
  // returns true if session is valid
  // returns false if session is invalid or expired
  const hasAccess = useMemo(() => {
    if (!shouldCheckAccess) {
      return undefined;
    }
    return isSessionValid;
  }, [shouldCheckAccess, isSessionValid]);

  const checkAccess = useCallback(() => {
    let timeoutId: ReturnType<typeof setTimeout>;
    if (!shouldCheckAccess) {
      return;
    }

    if (!isSessionValid) {
      pushToast({
        message: "Please login to continue.",
        status: TOAST_STATUSES.error,
      });
      timeoutId = setTimeout(() => {
        navigate("/auth");
      }, REDIRECT_DELAY);
    }
    return () => clearTimeout(timeoutId);
  }, [shouldCheckAccess, isSessionValid, navigate]);

  // checkAccess returns a cleanup function that clears any pending redirect timeouts
  useEffect(() => checkAccess(), [checkAccess]);

  return { checkAccess, hasAccess, loading };
};
