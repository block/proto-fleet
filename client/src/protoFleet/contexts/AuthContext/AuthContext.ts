import { createContext } from "react";

export interface AuthTokens {
  accessToken: { value: string; expiry: Date };
}

export const AuthContext = createContext({
  authTokens: {
    accessToken: { value: "", expiry: new Date() },
  },
  setAuthTokens: (tokens: AuthTokens) => {
    void tokens;
  },
});
