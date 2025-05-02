import { useCallback, useEffect, useState } from "react";

import { useAuthContext } from "./useAuthContext";

const useAccessToken = (shouldCheckAccess = true, currentPath?: string) => {
  const { authTokens } = useAuthContext();

  // returns undefined if access is not needed
  // returns true if access token is valid
  // returns false if refresh token is invalid
  const [hasAccess, setHasAccess] = useState<boolean | undefined>(undefined);
  //const navigate = useNavigate();

  const dateNow = new Date();
  const dateAccessToken = new Date(authTokens.accessToken.expiry);
  const isValidAccessToken = dateAccessToken > dateNow;

  const checkAccess = useCallback(() => {
    if (!shouldCheckAccess) {
      return;
    }

    if (currentPath === "/auth" || currentPath === "/signup") {
      return;
    }

    if (isValidAccessToken) {
      setHasAccess(true);
      return;
    } else {
      setHasAccess(false);
      // TODO: redirect to auth page, now is conflicting with onboarding
      //navigate("/auth");
    }
  }, [isValidAccessToken, shouldCheckAccess, currentPath]);

  useEffect(() => {
    checkAccess();
  }, [checkAccess]);

  return { checkAccess, hasAccess, setHasAccess };
};

export { useAccessToken };
