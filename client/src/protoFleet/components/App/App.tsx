import { type ErrorInfo, ReactNode, Suspense, useCallback, useEffect, useMemo, useRef } from "react";
import { useMatches } from "react-router-dom";
import clsx from "clsx";

import { onboardingClient } from "@/protoFleet/api/clients";
import AppLayout from "@/protoFleet/components/AppLayout";
import { requiresAuth } from "@/protoFleet/routeAuth";
import { globalRoutePrefetch } from "@/protoFleet/routePrefetch";
import type { ProtoFleetRouteHandle } from "@/protoFleet/routing/routeHandle";
import { useCheckAuthentication, useIsActionBarVisible } from "@/protoFleet/store";
import { useDeviceTheme, useSetDeviceTheme, useTheme } from "@/protoFleet/store";
import { redirectFromFleetDown } from "@/protoFleet/utils/fleetDownRedirect";
import ErrorBoundary from "@/shared/components/ErrorBoundary";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { useApplyTheme } from "@/shared/features/preferences";
import { Toaster } from "@/shared/features/toaster";
import { reportObservabilityError } from "@/shared/observability";
import { isBackendDownError } from "@/shared/utils/backendHealth";
import { prefetchRoutes } from "@/shared/utils/prefetchRoutes";

interface AppProps {
  children?: ReactNode;
  fullscreen?: boolean;
}

const App = ({ children, fullscreen }: AppProps) => {
  // ============================================================================
  // BACKEND HEALTH CHECK
  // ============================================================================
  const healthCheckDone = useRef(false);

  useEffect(() => {
    // Only run health check once on initial mount
    if (healthCheckDone.current) return;
    healthCheckDone.current = true;

    const isOnFleetDownErrorPage = window.location.pathname === "/fleet-down";
    let isMounted = true;

    // Check if backend is available by making a lightweight API call
    const checkBackendHealth = async () => {
      try {
        await onboardingClient.getFleetInitStatus({});

        // If backend is up and we're on the error page, redirect back to app
        if (isOnFleetDownErrorPage && isMounted) {
          redirectFromFleetDown();
        }
      } catch (error: unknown) {
        // Only redirect to error page if backend is down AND not already on error page
        if (isBackendDownError(error) && !isOnFleetDownErrorPage && isMounted) {
          const currentPath = window.location.pathname + window.location.search + window.location.hash;
          window.location.href = `/fleet-down?from=${encodeURIComponent(currentPath)}`;
        }
      }
    };

    checkBackendHealth();

    return () => {
      isMounted = false;
    };
  }, []);

  // ============================================================================
  // ROUTE CHUNK PREFETCH
  // ============================================================================
  // Warm sidebar-destination chunks at idle so the first nav click
  // resolves without a Suspense fallback.
  useEffect(() => {
    return prefetchRoutes(globalRoutePrefetch);
  }, []);

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
  const hideShellHeader = useMemo(
    () => matches.some((match) => (match.handle as ProtoFleetRouteHandle | undefined)?.hideShellHeader === true),
    [matches],
  );

  const { loading, hasAccess } = useCheckAuthentication(requireAuth);

  const isActionBarVisible = useIsActionBarVisible();

  // reportObservabilityError is a stable module-level import, not a reactive
  // value, so it is intentionally not a dependency (exhaustive-deps agrees).
  const handleRenderError = useCallback((error: Error, errorInfo: ErrorInfo) => {
    reportObservabilityError(error, { componentStack: errorInfo.componentStack });
  }, []);

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
    <ErrorBoundary onError={handleRenderError}>
      {/* Toaster - Fixed position, renders above overlays (z-50) and dialogs (z-40) */}
      <div
        className={clsx(
          "fixed right-4 z-60 transition-[bottom] duration-200 phone:right-2",
          isActionBarVisible ? "bottom-24 phone:bottom-30 tablet-only:bottom-30" : "bottom-4 phone:bottom-2",
        )}
      >
        <Toaster />
      </div>

      <Suspense
        fallback={
          <div className="flex min-h-screen items-center justify-center">
            <ProgressCircular indeterminate />
          </div>
        }
      >
        {fullscreen ? (
          // Fullscreen mode: Just render children without AppLayout chrome
          children
        ) : (
          // Normal mode: Render with AppLayout
          <AppLayout hideShellHeader={hideShellHeader}>{children}</AppLayout>
        )}
      </Suspense>
    </ErrorBoundary>
  );
};

export default App;
