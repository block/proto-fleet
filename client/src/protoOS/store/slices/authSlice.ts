import type { StateCreator } from "zustand";
import type { MinerStore } from "../useMinerStore";

// =============================================================================
// Auth Types
// =============================================================================

export interface AuthTokens {
  accessToken: { value: string; expiry: Date };
  refreshToken: { value: string; expiry: Date };
}

// =============================================================================
// Auth Slice Interface
// =============================================================================

export interface AuthSlice {
  // State
  authTokens: AuthTokens;
  loading: boolean;

  // Actions
  setAuthTokens: (tokens: AuthTokens) => void;
  setLoading: (loading: boolean) => void;
  logout: () => void;
}

// =============================================================================
// Auth Slice Implementation
// =============================================================================

const nullAuthTokens: AuthTokens = {
  accessToken: { value: "", expiry: new Date() },
  refreshToken: { value: "", expiry: new Date() },
};

export const createAuthSlice: StateCreator<MinerStore, [["zustand/immer", never]], [], AuthSlice> = (set) => ({
  // Initial State
  authTokens: nullAuthTokens,
  loading: true,

  // Actions
  setAuthTokens: (tokens) =>
    set((state) => {
      state.auth.authTokens = tokens;
    }),

  setLoading: (loading) =>
    set((state) => {
      state.auth.loading = loading;
    }),

  logout: () =>
    set((state) => {
      state.auth.authTokens = nullAuthTokens;
      state.auth.loading = false;
    }),
});
