import { useEffect, useState } from "react";
import { RouterProvider } from "react-router-dom";

import { themes } from "common/constants";
import { AuthContext, AuthTokens } from "common/contexts/AuthContext";
import { useLocalStorage } from "common/hooks/useLocalStorage";

import router from "./router";

import "./index.css";

const Main = () => {
  const [showLoginModal, setShowLoginModal] = useState(false);
  const { getItem, setItem } = useLocalStorage();
  const [authTokens, setAuthTokens] = useState({
    accessToken: getItem("accessToken") || { value: "", expiry: new Date() },
    refreshToken: getItem("refreshToken") || { value: "", expiry: new Date() },
  });
  const [dismissedLoginModal, setDismissedLoginModal] = useState(false);

  const setTheme = (isDark: boolean) => {
    const theme = isDark ? themes.dark : themes.light;
    document.body.setAttribute("data-theme", theme);
  };

  useEffect(() => {
    const darkThemeMq = window.matchMedia("(prefers-color-scheme: dark)");
    setTheme(darkThemeMq.matches);

    darkThemeMq.addEventListener("change", (e) => {
      setTheme(e.matches);
    });
  }, []);

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
      <RouterProvider router={router} />
    </AuthContext.Provider>
  );
};

export default Main;
