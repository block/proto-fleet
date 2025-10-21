import { useMemo } from "react";
import { Outlet, useMatches } from "react-router-dom";

import AppLayout from "@/protoFleet/components/AppLayout";
import { getRouteMetadata } from "@/protoFleet/routes";
import { useIsAuthenticated } from "@/protoFleet/store";
import {
  useDeviceTheme,
  useSetDeviceTheme,
  useTheme,
} from "@/protoFleet/store";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { useApplyTheme } from "@/shared/features/preferences";
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

  const { loading, hasAccess } = useIsAuthenticated(requireAuth);

  // Show loading spinner while checking auth or if access is denied (redirect in progress)
  const showLoading = loading || (requireAuth && hasAccess !== true);

  return (
    <>
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <Toaster />
      </div>

      {metadata.useAppLayout ? (
        showLoading ? (
          <div className="flex min-h-screen items-center justify-center">
            <ProgressCircular indeterminate />
          </div>
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
  const theme = useTheme();
  const deviceTheme = useDeviceTheme();
  const setDeviceTheme = useSetDeviceTheme();

  // Apply theme effects on mount
  useApplyTheme({ theme, deviceTheme, setDeviceTheme });

  return <AppContent />;
};

export default App;
