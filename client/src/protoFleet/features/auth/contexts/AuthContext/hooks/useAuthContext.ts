import { useContext } from "react";

import { AuthContext } from "../AuthContext";

const useAuthContext = () => {
  const { authTokens, setAuthTokens } = useContext(AuthContext);

  return {
    authTokens,
    setAuthTokens,
  };
};

export { useAuthContext };
