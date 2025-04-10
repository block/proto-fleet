import { useState } from "react";
import { RouterProvider } from "react-router-dom";

import router from "./router";
import { AuthContext, AuthTokens } from "@/protoOS/contexts/AuthContext";
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import { PreferencesProvider } from "@/shared/features/preferences/PreferencesContext";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";

import "@/shared/styles/index.css";

const Main = () => {
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
    <MinerHostingProvider>
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
        <PreferencesProvider>
          <RouterProvider router={router} />
        </PreferencesProvider>
      </AuthContext.Provider>
    </MinerHostingProvider>
  );
};

export default Main;
