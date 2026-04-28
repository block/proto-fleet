import { useCallback, useEffect, useRef, useState } from "react";

import { onboardingClient } from "@/protoFleet/api/clients";
import LogoAlt from "@/shared/assets/icons/LogoAlt";
import AnimatedDotsBackground from "@/shared/components/Animation";
import Button, { variants } from "@/shared/components/Button";
import { usePoll } from "@/shared/hooks/usePoll";
import { isBackendDownError } from "@/shared/utils/backendHealth";
import { redirectFromFleetDown } from "@/protoFleet/utils/fleetDownRedirect";

const FleetDown = () => {
  const [isChecking, setIsChecking] = useState(false);
  const isMounted = useRef(true);

  useEffect(() => {
    return () => {
      isMounted.current = false;
    };
  }, []);

  const checkBackendStatus = useCallback(async (isManual: boolean) => {
    if (isManual) {
      setIsChecking(true);
    }

    try {
      // Check if backend is back up
      await onboardingClient.getFleetInitStatus({});
      // Backend is up - redirect back to app (only if still mounted)
      if (isMounted.current) {
        redirectFromFleetDown();
      }
    } catch (error: unknown) {
      // Backend still down - stay on error page
      if (isBackendDownError(error)) {
        // Show user that backend is still down (only if still mounted)
        if (isMounted.current && isManual) {
          setIsChecking(false);
        }
      } else {
        // Some other error - try redirecting anyway (only if still mounted)
        if (isMounted.current) {
          redirectFromFleetDown();
        }
      }
    }
  }, []);

  const handleManualRetry = useCallback(() => {
    checkBackendStatus(true);
  }, [checkBackendStatus]);

  const performAutomaticCheck = useCallback(async () => {
    await checkBackendStatus(false);
  }, [checkBackendStatus]);

  usePoll({
    fetchData: performAutomaticCheck,
    poll: true,
    pollIntervalMs: 15 * 1000, // 15 seconds
  });

  return (
    <div className="relative flex h-screen flex-col items-center justify-center overflow-hidden bg-surface-base">
      {/* Main content - centered */}
      <div className="flex max-w-[480px] flex-col items-center gap-10">
        <div className="text-text-primary">
          <LogoAlt width="w-[62px]" />
        </div>

        <div className="flex flex-col items-center gap-1 text-center">
          <h1 className="text-display-200 text-text-primary">Fleet will be right back</h1>
          <p className="text-400 text-text-primary-70">
            We’re working to resolve this issue. You can try again later, and we will periodically check for Fleet
            availability for you.
          </p>
        </div>

        <Button onClick={handleManualRetry} loading={isChecking} variant={variants.secondary}>
          {isChecking ? "Retrying" : "Retry now"}
        </Button>
      </div>

      {/* Animated dots background at bottom - absolutely positioned */}
      <div className="absolute right-0 bottom-0 left-0 h-[21.1vh] p-10">
        <AnimatedDotsBackground padding={0} />
      </div>
    </div>
  );
};

export default FleetDown;
