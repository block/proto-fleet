import type { StateCreator } from "zustand";

import type { AuthState } from "../contract";
import type { MinerStore } from "../useMinerStore";

export interface AuthTokens {
  accessToken: { value: string; expiry: Date };
  refreshToken: { value: string; expiry: Date };
}

export interface AuthSlice {
  session: AuthState;
  setAuthTokens: (tokens: AuthTokens) => void;
  setLoading: (loading: boolean) => void;
  logout: () => void;
}

const unauthenticated = (status: "loading" | "unauthenticated" = "unauthenticated"): AuthState => ({
  mode: "bearer",
  status,
});

export const authTokensView = (session: AuthState | undefined): AuthTokens => ({
  accessToken: {
    value: session && "accessToken" in session && session.accessToken ? session.accessToken.value : "",
    expiry: new Date(session && "accessToken" in session && session.accessToken ? session.accessToken.expiresAtMs : 0),
  },
  refreshToken: {
    value: session && "refreshToken" in session && session.refreshToken ? session.refreshToken.value : "",
    expiry: new Date(
      session && "refreshToken" in session && session.refreshToken ? session.refreshToken.expiresAtMs : 0,
    ),
  },
});

export const authTokensFromSlice = (auth: AuthSlice | { session?: AuthState; authTokens?: AuthTokens }): AuthTokens =>
  auth.session
    ? authTokensView(auth.session)
    : "authTokens" in auth
      ? (auth.authTokens ?? authTokensView(undefined))
      : authTokensView(undefined);

export const createAuthSlice: StateCreator<MinerStore, [["zustand/immer", never]], [], AuthSlice> = (set) => ({
  session: unauthenticated("loading"),
  setAuthTokens: (tokens) =>
    set((state) => {
      state.auth.session = {
        mode: "bearer",
        status: "authenticated",
        accessToken: { value: tokens.accessToken.value, expiresAtMs: new Date(tokens.accessToken.expiry).getTime() },
        refreshToken: { value: tokens.refreshToken.value, expiresAtMs: new Date(tokens.refreshToken.expiry).getTime() },
      };
    }),
  setLoading: (loading) =>
    set((state) => {
      const session = state.auth.session;
      if (
        loading &&
        session.mode === "bearer" &&
        (session.status === "authenticated" || session.status === "refreshing")
      )
        session.status = "refreshing";
      else if (loading) state.auth.session = unauthenticated("loading");
      else if (session.mode === "bearer" && session.status === "refreshing") session.status = "authenticated";
      else if (session.mode === "bearer" && session.status === "loading") state.auth.session = unauthenticated();
    }),
  logout: () =>
    set((state) => {
      if (state.auth.session.mode === "bearer" && state.auth.session.status === "unauthenticated") return;
      state.auth.session = unauthenticated();
    }),
});
