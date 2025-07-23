import { ReactNode, useEffect, useState } from "react";
import {
  AuthContext,
  AuthTokens,
} from "@/protoFleet/features/auth/contexts/AuthContext";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";

type AuthProviderProps = {
  children: ReactNode;
};

const MIN_LOADING_TIME = 500;

export const AuthProvider = ({ children }: AuthProviderProps) => {
  const { getItem, setItem } = useLocalStorage();
  const [authTokens, setAuthTokens] = useState({
    accessToken: getItem("accessToken") || {
      value: "",
      expiry: new Date(),
    },
  });
  const [username, setUsername] = useState<string>(getItem("username") || "");
  const [loading, setLoading] = useState(true);

  const handleChangeAuthTokens = (newAuthTokens: AuthTokens) => {
    setAuthTokens(newAuthTokens);
    setItem("accessToken", newAuthTokens.accessToken);
  };

  const handleChangeUsername = (newUsername: string) => {
    setUsername(newUsername);
    setItem("username", newUsername);
  };

  useEffect(() => {
    const timeout = setTimeout(() => {
      setLoading(false);
    }, MIN_LOADING_TIME);
    return () => clearTimeout(timeout);
  }, []);

  return (
    <AuthContext.Provider
      value={{
        authTokens,
        setAuthTokens: handleChangeAuthTokens,
        username,
        setUsername: handleChangeUsername,
        loading,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
};
