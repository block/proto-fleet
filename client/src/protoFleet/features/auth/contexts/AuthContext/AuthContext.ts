import { createContext } from "react";
import { AuthTokens } from "@/protoFleet/features/auth/contexts/AuthContext";

type AuthContextType = {
  authTokens: AuthTokens;
  setAuthTokens: (tokens: AuthTokens) => void;
  username: string;
  setUsername: (username: string) => void;
  loading: boolean;
};

export const AuthContext = createContext<AuthContextType>({
  authTokens: {
    accessToken: { value: "", expiry: new Date() },
  },
  setAuthTokens: (tokens: AuthTokens) => {
    void tokens;
  },
  username: "",
  setUsername: (username: string) => {
    void username;
  },
  loading: true,
});
