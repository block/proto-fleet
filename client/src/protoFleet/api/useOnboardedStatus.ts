import { useCallback, useEffect } from "react";
import { onboardingClient } from "@/protoFleet/api/clients";
import type { FleetOnboardingStatus } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";
import {
  useAuthErrors,
  useAuthHeader,
  useAuthTokens,
  useDevicePaired,
  usePoolConfigured,
  useSetOnboardingStatus,
} from "@/protoFleet/store";

const useOnboardedStatus = () => {
  const authTokens = useAuthTokens();
  const authHeader = useAuthHeader();
  const poolConfigured = usePoolConfigured();
  const devicePaired = useDevicePaired();
  const setStatus = useSetOnboardingStatus();
  const { handleAuthErrors } = useAuthErrors();

  const fetchStatus = useCallback(async (): Promise<FleetOnboardingStatus | null> => {
    try {
      const response = await onboardingClient.getFleetOnboardingStatus({}, authHeader);

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
  }, [authHeader, setStatus, handleAuthErrors]);

  useEffect(() => {
    if (!authTokens.accessToken.value) {
      setStatus(null);
      return;
    }

    fetchStatus();
  }, [fetchStatus, authTokens, setStatus]);

  return {
    poolConfigured,
    devicePaired,
    refetch: fetchStatus,
  };
};

export { useOnboardedStatus };
