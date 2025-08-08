import { useContext } from "react";

import { AuthContext } from "../AuthContext";

const useAuthContext = () => {
  const { authTokens, setAuthTokens, username, setUsername, loading, logout } =
    useContext(AuthContext);

  return {
    authTokens,
    setAuthTokens,
    username,
    setUsername,
    loading,
    logout,
  };
};

export { useAuthContext };
