import { useContext } from "react";

import { AuthContext } from "../AuthContext";

const useAuthContext = () => {
  const { authTokens, setAuthTokens, username, setUsername, loading } =
    useContext(AuthContext);

  return {
    authTokens,
    setAuthTokens,
    username,
    setUsername,
    loading,
  };
};

export { useAuthContext };
