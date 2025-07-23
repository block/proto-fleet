import { useCallback, useEffect, useState } from "react";

import { useNavigate } from "react-router-dom";
import { useAuthContext } from "./useAuthContext";
import {
  pushToast,
  STATUSES as TOAST_STATUSES,
} from "@/shared/features/toaster";

const REDIRECT_DELAY = 600;

const useIsAuthenticated = (shouldCheckAccess = true) => {
  const { authTokens, loading } = useAuthContext();

  // returns undefined if access is not needed
  // returns true if access token is valid
  // returns false if refresh token is invalid
  const [hasAccess, setHasAccess] = useState<boolean | undefined>(undefined);
  const navigate = useNavigate();

  const dateNow = new Date();
  const dateAccessToken = new Date(authTokens.accessToken.expiry);
  const isValidAccessToken = dateAccessToken > dateNow;

  const checkAccess = useCallback(() => {
    let timeoutId: ReturnType<typeof setTimeout>;
    if (!shouldCheckAccess) {
      return;
    }

    if (isValidAccessToken) {
      setHasAccess(true);
      return;
    } else {
      setHasAccess(false);
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

  return { checkAccess, hasAccess, setHasAccess, loading };
};

export { useIsAuthenticated };
