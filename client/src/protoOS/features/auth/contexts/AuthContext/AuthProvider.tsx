import { ReactNode, useState } from "react";
import {
  AuthContext,
  AuthTokens,
} from "@/protoOS/features/auth/contexts/AuthContext";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";

type AuthProviderProps = {
  children: ReactNode;
};

export const AuthProvider = ({ children }: AuthProviderProps) => {
  const [showLoginModal, setShowLoginModal] = useState(false);
  const { getItem, setItem } = useLocalStorage();
  const [authTokens, setAuthTokens] = useState({
    accessToken: getItem("accessToken") || { value: "", expiry: new Date() },
    refreshToken: getItem("refreshToken") || { value: "", expiry: new Date() },
  });
  const [dismissedLoginModal, setDismissedLoginModal] = useState(false);

  const handleChangeAuthTokens = (newAuthTokens: AuthTokens) => {
    setAuthTokens(newAuthTokens);
    setItem("accessToken", newAuthTokens.accessToken);
    setItem("refreshToken", newAuthTokens.refreshToken);
  };

  const handleChangeLoginModal = (show: boolean) => {
    setShowLoginModal(show);
    if (show) {
      setDismissedLoginModal(false);
    }
  };

  return (
    <AuthContext.Provider
      value={{
        authTokens,
        setAuthTokens: handleChangeAuthTokens,
        showLoginModal,
        setShowLoginModal: handleChangeLoginModal,
        dismissedLoginModal,
        setDismissedLoginModal,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
};
