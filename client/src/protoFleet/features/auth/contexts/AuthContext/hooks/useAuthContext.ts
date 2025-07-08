import { useContext } from "react";

import { AuthContext } from "../AuthContext";

const useAuthContext = () => {
  const { authTokens, setAuthTokens, username, setUsername } =
    useContext(AuthContext);

  return {
    authTokens,
    setAuthTokens,
    username,
    setUsername,
  };
};

export { useAuthContext };
