import { useCallback, useEffect } from "react";
import { onboardingClient } from "@/protoFleet/api/clients";
import type { FleetOnboardingStatus } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";
import {
  useAuthErrors,
  useDevicePaired,
  useIsAuthenticated,
  usePoolConfigured,
  useSetOnboardingStatus,
} from "@/protoFleet/store";

const useOnboardedStatus = () => {
  const isAuthenticated = useIsAuthenticated();
  const poolConfigured = usePoolConfigured();
  const devicePaired = useDevicePaired();
  const setStatus = useSetOnboardingStatus();
  const { handleAuthErrors } = useAuthErrors();

  const fetchStatus = useCallback(async (): Promise<FleetOnboardingStatus | null> => {
    try {
      const response = await onboardingClient.getFleetOnboardingStatus({});

      if (response.status) {
        setStatus(response.status);
        return response.status;
      }
      return null;
    } catch (err: any) {
      handleAuthErrors({
        error: err,
        onError: () => {
          const errorMessage = err?.message ?? String(err);
          throw new Error(`Failed to fetch Onboarded Status: ${errorMessage}`);
        },
      });
      return null;
    }
  }, [setStatus, handleAuthErrors]);

  useEffect(() => {
    if (!isAuthenticated) {
      setStatus(null);
      return;
    }

    fetchStatus();
  }, [fetchStatus, isAuthenticated, setStatus]);

  return {
    poolConfigured,
    devicePaired,
    refetch: fetchStatus,
  };
};

export { useOnboardedStatus };
