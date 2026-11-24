import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";

import { useFleetStore } from "@/protoFleet/store";

// Only recovery identifiers and display/selection metadata belong here. Never
// retain passwords, pool slots, or the full review/confirmation request.
export type ConfirmationDetails = {
  reviewId: string;
  configurationId: string;
  name: string;
  deviceIdentifiers: string[];
  kind: "worker_names" | "pool_configuration";
};
export type ConfirmationAttempt = ConfirmationDetails & {
  id: string;
  username: string;
  accountId: string;
  requestId: string;
  status: "submitting" | "unknown";
};

export const useConfirmationRecovery = create(
  persist<{ attempts: ConfirmationAttempt[] }>(() => ({ attempts: [] }), {
    name: "proto-pool-confirmation-recovery",
    storage: createJSONStorage(() => sessionStorage),
    // Reloading loses the live request, not the need to recover its outcome.
    partialize: ({ attempts }) => ({ attempts: attempts.map((a) => ({ ...a, status: "unknown" })) }),
  }),
);

export const startConfirmationAttempt = (username: string, accountId: string, details: ConfirmationDetails) => {
  const id = JSON.stringify([username, accountId, details.reviewId]);
  const previous = useConfirmationRecovery.getState().attempts.find((a) => a.id === id);
  const attempt: ConfirmationAttempt = {
    id,
    username,
    accountId,
    requestId: crypto.randomUUID(),
    status: "submitting",
    reviewId: details.reviewId,
    configurationId: details.configurationId,
    name: details.name,
    deviceIdentifiers: [...details.deviceIdentifiers],
    kind: details.kind,
  };
  useConfirmationRecovery.setState(({ attempts }) => ({ attempts: [...attempts.filter((a) => a.id !== id), attempt] }));
  return { attempt, wasUnknown: previous?.status === "unknown" };
};

export const markConfirmationUnknown = (attempt: ConfirmationAttempt) =>
  useConfirmationRecovery.setState(({ attempts }) => ({
    attempts: attempts.map((a) =>
      a.id === attempt.id && a.requestId === attempt.requestId ? { ...a, status: "unknown" } : a,
    ),
  }));
export const forgetConfirmation = (attempt: ConfirmationAttempt) =>
  useConfirmationRecovery.setState(({ attempts }) => ({
    attempts: attempts.filter((a) => a.id !== attempt.id || a.requestId !== attempt.requestId),
  }));

useFleetStore.subscribe((state, previous) => {
  if (state.auth.sessionGeneration !== previous.auth.sessionGeneration) {
    useConfirmationRecovery.setState(({ attempts }) => ({
      attempts: attempts.map((a) => (a.status === "submitting" ? { ...a, status: "unknown" } : a)),
    }));
  }
});
