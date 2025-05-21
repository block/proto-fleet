import { useCallback, useEffect, useState } from "react";

import { useNavigate } from "react-router-dom";
import { useAuthContext } from "./useAuthContext";

const useIsAuthenticated = (shouldCheckAccess = true) => {
  const { authTokens } = useAuthContext();

  // returns undefined if access is not needed
  // returns true if access token is valid
  // returns false if refresh token is invalid
  const [hasAccess, setHasAccess] = useState<boolean | undefined>(undefined);
  const navigate = useNavigate();

  const dateNow = new Date();
  const dateAccessToken = new Date(authTokens.accessToken.expiry);
  const isValidAccessToken = dateAccessToken > dateNow;

  const checkAccess = useCallback(() => {
    if (!shouldCheckAccess) {
      return;
    }

    if (isValidAccessToken) {
      setHasAccess(true);
      return;
    } else {
      setHasAccess(false);
      navigate("/welcome");
    }
  }, [shouldCheckAccess, isValidAccessToken, navigate]);

  useEffect(() => {
    checkAccess();
  }, [checkAccess]);

  return { checkAccess, hasAccess, setHasAccess };
};

export { useIsAuthenticated };
