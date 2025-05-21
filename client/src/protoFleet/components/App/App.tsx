import { useMemo } from "react";
import { Outlet, useMatches } from "react-router-dom";

import AppLayout from "@/protoFleet/components/AppLayout";
import { useIsAuthenticated } from "@/protoFleet/features/auth/contexts/AuthContext";
// import { useCompleteOnboarding } from "@/protoFleet/features/onboarding";
import { getRouteMetadata } from "@/protoFleet/routes";

const App = () => {
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

  // TODO: Unsure on if we want want to call this hook here or not
  // This effects the UX for onboarding. Do we want to let users go back previously completed steps?
  // useCompleteOnboarding();

  return (
    <>
      {metadata.useAppLayout ? (
        <AppLayout title={metadata?.title || ""}>
          <Outlet />
        </AppLayout>
      ) : (
        <Outlet />
      )}
    </>
  );
};

export default App;
