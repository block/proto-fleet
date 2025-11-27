import type { StateCreator } from "zustand";
import type { FleetStore } from "../useFleetStore";
import type { FleetOnboardingStatus } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";

// =============================================================================
// Onboarding Slice Interface
// =============================================================================

export interface OnboardingSlice {
  poolConfigured: boolean;
  devicePaired: boolean;

  // Actions
  setStatus: (status: FleetOnboardingStatus | null) => void;
  setPoolConfigured: (configured: boolean) => void;
  setDevicePaired: (paired: boolean) => void;
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

  // Actions
  setStatus: (status) =>
    set((state) => {
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
});
