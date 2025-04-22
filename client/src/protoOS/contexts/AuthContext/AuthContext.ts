import { createContext } from "react";

export interface AuthTokens {
  accessToken: { value: string; expiry: Date };
  refreshToken: { value: string; expiry: Date };
}

export const AuthContext = createContext({
  authTokens: {
    accessToken: { value: "", expiry: new Date() },
    refreshToken: { value: "", expiry: new Date() },
  },
  setAuthTokens: (tokens: AuthTokens) => {
    void tokens;
  },
  showLoginModal: false,
  setShowLoginModal: (show: boolean) => {
    void show;
  },
  dismissedLoginModal: false,
  setDismissedLoginModal: (dismissed: boolean) => {
    void dismissed;
  },
});
