import { useMemo } from "react";
import { Outlet, useMatches } from "react-router-dom";

import AppLayout from "@/protoFleet/components/AppLayout";
import { useIsAuthenticated } from "@/protoFleet/features/auth/contexts/AuthContext";
import { OnboardingProvider } from "@/protoFleet/features/onboarding/contexts/OnboardingContext";
import { getRouteMetadata } from "@/protoFleet/routes";
import { Splash } from "@/shared/components/Splash";
import { Toaster } from "@/shared/features/toaster";

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

  const { loading } = useIsAuthenticated(requireAuth);

  return (
    <>
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <Toaster />
      </div>

      {metadata.useAppLayout ? (
        loading ? (
          <Splash />
        ) : (
          <AppLayout title={metadata?.title || ""}>
            <Outlet />
          </AppLayout>
        )
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
