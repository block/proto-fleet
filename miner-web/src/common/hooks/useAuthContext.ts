import { useContext } from "react";

import { AuthContext } from "common/contexts/AuthContext";

const useAuthContext = () => {
  const {
    authTokens,
    setAuthTokens,
    showLoginModal,
    setShowLoginModal,
    dismissedLoginModal,
    setDismissedLoginModal,
  } = useContext(AuthContext);

  return {
    authTokens,
    dismissedLoginModal,
    setAuthTokens,
    showLoginModal,
    setDismissedLoginModal,
    setShowLoginModal,
  };
};

export { useAuthContext };
