import type { StateCreator } from "zustand";
import type { FleetStore } from "../useFleetStore";

// =============================================================================
// Auth Slice Interface
// =============================================================================

export interface AuthSlice {
  sessionExpiry: Date | null;
  isAuthenticated: boolean;
  username: string;
  role: string;
  authLoading: boolean;
  temporaryPassword: string | null;

  // Actions
  setSessionExpiry: (expiry: Date | null) => void;
  setIsAuthenticated: (isAuthenticated: boolean) => void;
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
  sessionExpiry: null,
  isAuthenticated: false,
  username: "",
  role: "",
  authLoading: true,
  temporaryPassword: null,

  // Actions
  setSessionExpiry: (expiry) =>
    set((state) => {
      state.auth.sessionExpiry = expiry;
    }),

  setIsAuthenticated: (isAuthenticated) =>
    set((state) => {
      state.auth.isAuthenticated = isAuthenticated;
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
      state.auth.sessionExpiry = null;
      state.auth.isAuthenticated = false;
      state.auth.username = "";
      state.auth.role = "";
      state.auth.authLoading = false;
      state.auth.temporaryPassword = null;
    }),
});
