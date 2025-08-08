import { useContext } from "react";

import { AuthContext } from "../AuthContext";

const useAuthContext = () => {
  const {
    authTokens,
    setAuthTokens,
    showLoginModal,
    setShowLoginModal,
    dismissedLoginModal,
    setDismissedLoginModal,
    logout,
  } = useContext(AuthContext);

  return {
    authTokens,
    dismissedLoginModal,
    setAuthTokens,
    showLoginModal,
    setDismissedLoginModal,
    setShowLoginModal,
    logout,
  };
};

export { useAuthContext };
