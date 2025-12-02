import { ReactNode, useMemo } from "react";
import { useMatches } from "react-router-dom";

import AppLayout from "@/protoFleet/components/AppLayout";
import { requiresAuth } from "@/protoFleet/router";
import { useCheckAuthentication } from "@/protoFleet/store";
import { useDeviceTheme, useSetDeviceTheme, useTheme } from "@/protoFleet/store";
import ErrorBoundary from "@/shared/components/ErrorBoundary";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { useApplyTheme } from "@/shared/features/preferences";
import { Toaster } from "@/shared/features/toaster";

interface AppProps {
  children?: ReactNode;
  fullscreen?: boolean;
}

const App = ({ children, fullscreen }: AppProps) => {
  // ============================================================================
  // THEME APPLICATION
  // ============================================================================
  const theme = useTheme();
  const deviceTheme = useDeviceTheme();
  const setDeviceTheme = useSetDeviceTheme();

  // Apply theme effects on mount
  useApplyTheme({ theme, deviceTheme, setDeviceTheme });

  // ============================================================================
  // AUTH CHECKING
  // ============================================================================
  const matches = useMatches();
  const currentPath = useMemo(() => {
    return matches[matches.length - 1]?.pathname || "/";
  }, [matches]);

  const requireAuth = useMemo(() => {
    // Check if this specific path is configured to not require auth
    // If not in the config, default to requiring auth
    return requiresAuth[currentPath] !== false;
  }, [currentPath]);

  const { loading, hasAccess } = useCheckAuthentication(requireAuth);

  // Show loading spinner ONLY if auth is required AND (loading OR access denied)
  const showLoading = requireAuth && (loading || hasAccess !== true);

  // ============================================================================
  // LOADING STATE
  // ============================================================================
  if (showLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  // ============================================================================
  // RENDER
  // ============================================================================
  return (
    <ErrorBoundary>
      {/* Toaster - Fixed position, renders above everything */}
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <Toaster />
      </div>

      {fullscreen ? (
        // Fullscreen mode: Just render children without AppLayout chrome
        children
      ) : (
        // Normal mode: Render with AppLayout
        <AppLayout>{children}</AppLayout>
      )}
    </ErrorBoundary>
  );
};

export default App;
