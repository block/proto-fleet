import { useMemo } from "react";
import { Outlet, useMatches } from "react-router-dom";

import AppLayout from "@/protoFleet/components/AppLayout";
import CompleteOnboardingDialog from "@/protoFleet/components/CompleteOnboardingDialog";
import { useIsAuthenticated } from "@/protoFleet/features/auth/contexts/AuthContext";
import {
  OnboardingProvider,
  useOnboardingContext,
} from "@/protoFleet/features/onboarding/contexts/OnboardingContext/";
import { getRouteMetadata } from "@/protoFleet/routes";

const AppContent = () => {
  const matches = useMatches();
  const currentPath = useMemo(() => {
    return matches[matches.length - 1]?.pathname || "/";
  }, [matches]);

  const metadata = useMemo(() => {
    return getRouteMetadata(currentPath);
  }, [currentPath]);

  const requireAuth = useMemo(() => {
    return !(metadata?.requireAuth === false);
  }, [metadata]);

  useIsAuthenticated(requireAuth);
  const { status: onboardingStatus } = useOnboardingContext();
  const onboardingComplete = useMemo(() => {
    return (
      onboardingStatus === null ||
      (onboardingStatus?.devicePaired === true &&
        onboardingStatus?.poolConfigured === true)
    );
  }, [onboardingStatus]);

  return (
    <>
      {metadata.useAppLayout ? (
        <AppLayout title={metadata?.title || ""}>
          <Outlet />
          {!onboardingComplete && (
            <CompleteOnboardingDialog onboardingStatus={onboardingStatus} />
          )}
        </AppLayout>
      ) : (
        <Outlet />
      )}
    </>
  );
};

const App = () => {
  return (
    <OnboardingProvider>
      <AppContent />
    </OnboardingProvider>
  );
};

export default App;
