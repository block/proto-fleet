import { ReactNode, useState } from "react";
import {
  AuthContext,
  AuthTokens,
} from "@/protoFleet/features/auth/contexts/AuthContext";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";

type AuthProviderProps = {
  children: ReactNode;
};

export const AuthProvider = ({ children }: AuthProviderProps) => {
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
      {children}
    </AuthContext.Provider>
  );
};
