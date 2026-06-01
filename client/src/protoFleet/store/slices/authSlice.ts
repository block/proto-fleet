import type { StateCreator } from "zustand";
import { DEFAULT_ACTIVE_SITE } from "../types/activeSite";
import type { FleetStore } from "../useFleetStore";
import { resetActiveCurtailmentData } from "@/protoFleet/api/activeCurtailmentData";

// =============================================================================
// Auth Slice Interface
// =============================================================================

export interface AuthSlice {
  sessionExpiry: Date | null;
  isAuthenticated: boolean;
  username: string;
  role: string;
  // permissions is the caller's effective permission keys, populated
  // from UserInfo.permissions on login. UI gates query this via
  // useHasPermission; the server still enforces every gate.
  permissions: string[];
  authLoading: boolean;
  temporaryPassword: string | null;

  // Actions
  setSessionExpiry: (expiry: Date | null) => void;
  setIsAuthenticated: (isAuthenticated: boolean) => void;
  setUsername: (username: string) => void;
  setRole: (role: string) => void;
  setPermissions: (permissions: string[]) => void;
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
  permissions: [],
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

  setPermissions: (permissions) =>
    set((state) => {
      state.auth.permissions = permissions;
    }),

  setAuthLoading: (loading) =>
    set((state) => {
      state.auth.authLoading = loading;
    }),

  setTemporaryPassword: (password) =>
    set((state) => {
      state.auth.temporaryPassword = password;
    }),

  logout: () => {
    resetActiveCurtailmentData();
    set((state) => {
      state.auth.sessionExpiry = null;
      state.auth.isAuthenticated = false;
      state.auth.username = "";
      state.auth.role = "";
      state.auth.permissions = [];
      state.auth.authLoading = false;
      state.auth.temporaryPassword = null;
      // Reset multi-site active selection on logout so user B doesn't
      // inherit user A's choice on shared browsers. Server-side org
      // scoping already prevents data exposure; this is a UX-level
      // hygiene reset.
      state.ui.activeSite = DEFAULT_ACTIVE_SITE;
    });
  },
});
