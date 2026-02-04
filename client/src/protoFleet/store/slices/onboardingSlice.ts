import type { StateCreator } from "zustand";
import type { FleetStore } from "../useFleetStore";
import type { FleetOnboardingStatus } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";

// =============================================================================
// Onboarding Slice Interface
// =============================================================================

export interface OnboardingSlice {
  poolConfigured: boolean;
  devicePaired: boolean;
  statusLoaded: boolean;

  // Actions
  setStatus: (status: FleetOnboardingStatus | null) => void;
  setPoolConfigured: (configured: boolean) => void;
  setDevicePaired: (paired: boolean) => void;
  resetStatus: () => void;
}

// =============================================================================
// Onboarding Slice Creator
// =============================================================================

export const createOnboardingSlice: StateCreator<FleetStore, [["zustand/immer", never]], [], OnboardingSlice> = (
  set,
) => ({
  // Initial state
  poolConfigured: false,
  devicePaired: false,
  statusLoaded: false,

  // Actions
  setStatus: (status) =>
    set((state) => {
      state.onboarding.statusLoaded = true;
      if (status) {
        state.onboarding.poolConfigured = status.poolConfigured;
        state.onboarding.devicePaired = status.devicePaired;
      } else {
        state.onboarding.poolConfigured = false;
        state.onboarding.devicePaired = false;
      }
    }),

  setPoolConfigured: (configured) =>
    set((state) => {
      state.onboarding.poolConfigured = configured;
    }),

  setDevicePaired: (paired) =>
    set((state) => {
      state.onboarding.devicePaired = paired;
    }),

  resetStatus: () =>
    set((state) => {
      state.onboarding.poolConfigured = false;
      state.onboarding.devicePaired = false;
      state.onboarding.statusLoaded = false;
    }),
});
