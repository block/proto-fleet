import { useCallback, useEffect } from "react";
import { onboardingClient } from "@/protoFleet/api/clients";
import type { FleetOnboardingStatus } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import {
  useAuthErrors,
  useDevicePaired,
  useIsAuthenticated,
  useOnboardingStatusLoaded,
  usePoolConfigured,
  useResetOnboardingStatus,
  useSetOnboardingStatus,
} from "@/protoFleet/store";

const useOnboardedStatus = ({ enabled = true }: { enabled?: boolean } = {}) => {
  const isAuthenticated = useIsAuthenticated();
  const poolConfigured = usePoolConfigured();
  const devicePaired = useDevicePaired();
  const statusLoaded = useOnboardingStatusLoaded();
  const setStatus = useSetOnboardingStatus();
  const resetStatus = useResetOnboardingStatus();
  const { handleAuthErrors } = useAuthErrors();

  const fetchStatus = useCallback(async (): Promise<FleetOnboardingStatus | null> => {
    try {
      const response = await onboardingClient.getFleetOnboardingStatus({});
      setStatus(response.status ?? null);
      return response.status ?? null;
    } catch (err: any) {
      setStatus(null);
      handleAuthErrors({
        error: err,
        onError: () => {
          const errorMessage = getErrorMessage(err);
          throw new Error(`Failed to fetch Onboarded Status: ${errorMessage}`);
        },
      });
      return null;
    }
  }, [setStatus, handleAuthErrors]);

  useEffect(() => {
    if (!enabled) {
      return;
    }

    if (!isAuthenticated) {
      resetStatus();
      return;
    }

    fetchStatus();
  }, [enabled, fetchStatus, isAuthenticated, resetStatus]);

  return {
    poolConfigured,
    devicePaired,
    statusLoaded,
    refetch: fetchStatus,
  };
};

export { useOnboardedStatus };
