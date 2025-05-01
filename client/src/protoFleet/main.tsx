import { useState } from "react";
import { RouterProvider } from "react-router-dom";

import router from "./router";
import {
  AuthContext,
  type AuthTokens,
} from "@/protoFleet/contexts/AuthContext";
import { PreferencesProvider } from "@/shared/features/preferences/PreferencesContext";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";
import "@/shared/styles/index.css";

const Main = () => {
  const { getItem, setItem } = useLocalStorage();
  const [authTokens, setAuthTokens] = useState({
    accessToken: getItem("accessToken") || {
      value: "",
      expiry: new Date(),
    },
  });

  const handleChangeAuthTokens = (newAuthTokens: AuthTokens) => {
    setAuthTokens(newAuthTokens);
    setItem("accessToken", newAuthTokens.accessToken);
  };

  return (
    <AuthContext.Provider
      value={{
        authTokens,
        setAuthTokens: handleChangeAuthTokens,
      }}
    >
      <PreferencesProvider>
        <RouterProvider router={router} />
      </PreferencesProvider>
    </AuthContext.Provider>
  );
};

export default Main;
