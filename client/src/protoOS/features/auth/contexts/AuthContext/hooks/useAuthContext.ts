import { useContext } from "react";

import { AuthContext } from "../AuthContext";

const useAuthContext = () => {
  const {
    authTokens,
    setAuthTokens,
    pausedAuthAction,
    setPausedAuthAction,
    showLoginModal,
    setShowLoginModal,
    dismissedLoginModal,
    setDismissedLoginModal,
    logout,
  } = useContext(AuthContext);

  return {
    authTokens,
    dismissedLoginModal,
    pausedAuthAction,
    setPausedAuthAction,
    setAuthTokens,
    showLoginModal,
    setDismissedLoginModal,
    setShowLoginModal,
    logout,
  };
};

export { useAuthContext };
