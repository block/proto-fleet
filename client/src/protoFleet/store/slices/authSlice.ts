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
  role: string;
  authLoading: boolean;
  temporaryPassword: string | null;

  // Actions
  setAuthTokens: (tokens: AuthTokens) => void;
  setUsername: (username: string) => void;
  setRole: (role: string) => void;
  setAuthLoading: (loading: boolean) => void;
  setTemporaryPassword: (password: string | null) => void;
  logout: () => void;
}

// =============================================================================
// Auth Slice Creator
// =============================================================================

export const createAuthSlice: StateCreator<FleetStore, [["zustand/immer", never]], [], AuthSlice> = (set) => ({
  // Initial state
  authTokens: {
    accessToken: { value: "", expiry: new Date() },
  },
  username: "",
  role: "",
  authLoading: true,
  temporaryPassword: null,

  // Actions
  setAuthTokens: (tokens) =>
    set((state) => {
      state.auth.authTokens = tokens;
    }),

  setUsername: (username) =>
    set((state) => {
      state.auth.username = username;
    }),

  setRole: (role) =>
    set((state) => {
      state.auth.role = role;
    }),

  setAuthLoading: (loading) =>
    set((state) => {
      state.auth.authLoading = loading;
    }),

  setTemporaryPassword: (password) =>
    set((state) => {
      state.auth.temporaryPassword = password;
    }),

  logout: () =>
    set((state) => {
      state.auth.authTokens = {
        accessToken: { value: "", expiry: new Date() },
      };
      state.auth.username = "";
      state.auth.role = "";
      state.auth.authLoading = false;
      state.auth.temporaryPassword = null;
    }),
});
