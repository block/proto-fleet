import { useCallback, useEffect, useState } from "react";
import { onboardingClient } from "@/protoFleet/api/clients";
import type { FleetOnboardingStatus } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";
import { useAuthContext } from "@/protoFleet/contexts/AuthContext";
import { getAuthHeader } from "@/protoFleet/contexts/AuthContext";

const useOnboardedStatus = () => {
  const { authTokens } = useAuthContext();
  const [status, setStatus] = useState<FleetOnboardingStatus | null>(null);

  const fetchStatus =
    useCallback(async (): Promise<FleetOnboardingStatus | null> => {
      try {
        const response = await onboardingClient.getFleetOnboardingStatus(
          {},
          getAuthHeader(authTokens),
        );

        if (response.status) {
          setStatus(response.status);
          return response.status;
        }
        return null;
      } catch (err: any) {
        const errorMessage = err?.error?.message ?? String(err);
        throw new Error(`Failed to fetch Onboarded Status: ${errorMessage}`);
      }
    }, [authTokens]);

  useEffect(() => {
    if (!authTokens.accessToken.value) {
      setStatus(null);
      return;
    }

    fetchStatus();
  }, [fetchStatus, authTokens]);

  return status ?? null;
};

export { useOnboardedStatus };
