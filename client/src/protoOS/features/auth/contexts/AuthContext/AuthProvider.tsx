import { ReactNode, useState } from "react";
import {
  AuthContext,
  AuthTokens,
} from "@/protoOS/features/auth/contexts/AuthContext";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";

type AuthProviderProps = {
  children: ReactNode;
};

const nullAuthTokens = {
  accessToken: { value: "", expiry: new Date() },
  refreshToken: { value: "", expiry: new Date() },
};

export const AuthProvider = ({ children }: AuthProviderProps) => {
  const [showLoginModal, setShowLoginModal] = useState(false);
  const { getItem, setItem } = useLocalStorage();
  const [authTokens, setAuthTokens] = useState({
    accessToken: getItem("accessToken") || nullAuthTokens.accessToken,
    refreshToken: getItem("refreshToken") || nullAuthTokens.refreshToken,
  });
  const [dismissedLoginModal, setDismissedLoginModal] = useState(false);
  const [loading, setLoading] = useState(true);

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

  const handleLogout = () => {
    setAuthTokens(nullAuthTokens);
    setItem("accessToken", nullAuthTokens.accessToken);
    setItem("refreshToken", nullAuthTokens.refreshToken);
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
        loading,
        setLoading,
        logout: handleLogout,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
};
