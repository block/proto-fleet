import { createContext } from "react";
import { AUTH_ACTIONS } from "./constants";

export interface AuthTokens {
  accessToken: { value: string; expiry: Date };
  refreshToken: { value: string; expiry: Date };
}

export type AuthActions = keyof typeof AUTH_ACTIONS | null;

export const AuthContext = createContext({
  authTokens: {
    accessToken: { value: "", expiry: new Date() },
    refreshToken: { value: "", expiry: new Date() },
  },
  logout: () => {
    void null;
  },
  setAuthTokens: (tokens: AuthTokens) => {
    void tokens;
  },
  pausedAuthAction: null as AuthActions,
  setPausedAuthAction: (action: AuthActions) => {
    void action;
  },
  showLoginModal: false,
  setShowLoginModal: (show: boolean) => {
    void show;
  },
  dismissedLoginModal: false,
  setDismissedLoginModal: (dismissed: boolean) => {
    void dismissed;
  },
  loading: true,
  setLoading: (loading: boolean) => {
    void loading;
  },
});
