import type { StateCreator } from "zustand";
import type { FleetStore } from "../useFleetStore";

// =============================================================================
// Auth Types
// =============================================================================

export interface AuthTokens {
  accessToken: { value: string; expiry: Date };
}

// =============================================================================
// Auth Slice Interface
// =============================================================================

export interface AuthSlice {
  authTokens: AuthTokens;
  username: string;
  authLoading: boolean;

  // Actions
  setAuthTokens: (tokens: AuthTokens) => void;
  setUsername: (username: string) => void;
  setAuthLoading: (loading: boolean) => void;
  logout: () => void;
}

// =============================================================================
// Auth Slice Creator
// =============================================================================

export const createAuthSlice: StateCreator<
  FleetStore,
  [["zustand/immer", never]],
  [],
  AuthSlice
> = (set) => ({
  // Initial state
  authTokens: {
    accessToken: { value: "", expiry: new Date() },
  },
  username: "",
  authLoading: true,

  // Actions
  setAuthTokens: (tokens) =>
    set((state) => {
      state.auth.authTokens = tokens;
    }),

  setUsername: (username) =>
    set((state) => {
      state.auth.username = username;
    }),

  setAuthLoading: (loading) =>
    set((state) => {
      state.auth.authLoading = loading;
    }),

  logout: () =>
    set((state) => {
      state.auth.authTokens = {
        accessToken: { value: "", expiry: new Date() },
      };
      state.auth.username = "";
      state.auth.authLoading = false;
    }),
});
