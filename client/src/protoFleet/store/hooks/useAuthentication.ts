import { useCallback, useEffect, useMemo } from "react";
import { useNavigate } from "react-router-dom";
import type { AuthTokens } from "../slices/authSlice";
import { useAuthLoading, useAuthTokens } from "./useAuth";
import {
  pushToast,
  STATUSES as TOAST_STATUSES,
} from "@/shared/features/toaster";

// =============================================================================
// Auth Utility Functions
// =============================================================================

export const getAuthHeader = (authTokens: AuthTokens) => {
  return {
    headers: { Authorization: `Bearer ${authTokens.accessToken.value}` },
  };
};

// =============================================================================
// Auth Access Hook
// =============================================================================

const REDIRECT_DELAY = 600;

export const useIsAuthenticated = (shouldCheckAccess = true) => {
  const authTokens = useAuthTokens();
  const loading = useAuthLoading();
  const navigate = useNavigate();

  const dateNow = new Date();
  const dateAccessToken = new Date(authTokens.accessToken.expiry);
  const isValidAccessToken = dateAccessToken > dateNow;

  // Derive hasAccess directly from token validity
  // returns undefined if access check is disabled
  // returns true if access token is valid
  // returns false if access token is invalid
  const hasAccess = useMemo(() => {
    if (!shouldCheckAccess) {
      return undefined;
    }
    return isValidAccessToken;
  }, [shouldCheckAccess, isValidAccessToken]);

  const checkAccess = useCallback(() => {
    let timeoutId: ReturnType<typeof setTimeout>;
    if (!shouldCheckAccess) {
      return;
    }

    if (!isValidAccessToken) {
      pushToast({
        message: "Please login to continue.",
        status: TOAST_STATUSES.error,
      });
      timeoutId = setTimeout(() => {
        navigate("/auth");
      }, REDIRECT_DELAY);
    }
    return () => clearTimeout(timeoutId);
  }, [shouldCheckAccess, isValidAccessToken, navigate]);

  useEffect(() => {
    checkAccess();
  }, [checkAccess]);

  return { checkAccess, hasAccess, loading };
};
