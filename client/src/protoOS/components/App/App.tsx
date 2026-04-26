import { ComponentType, ReactNode, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation } from "react-router-dom";

import AuthenticatedShell from "./AuthenticatedShell";
import ErrorCallout from "./ErrorCallout";
import WakeCallout from "./WakeCallout";
import WarmingUpCallout from "./WarmingUpCallout";
import { useMiningStart } from "@/protoOS/api/hooks/useMiningStart";
import { useMiningStatus } from "@/protoOS/api/hooks/useMiningStatus";
import { useSystemInfo } from "@/protoOS/api/hooks/useSystemInfo";
import { useSystemStatus } from "@/protoOS/api/hooks/useSystemStatus";
import AppLayout from "@/protoOS/components/AppLayout";
import DefaultContentLayout from "@/protoOS/components/ContentLayout/DefaultContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import { navigationMenuTypes } from "@/protoOS/components/NavigationMenu";
import NoPoolsCallout from "@/protoOS/components/NoPoolsCallout";
import { getNoPoolsCalloutState } from "@/protoOS/components/NoPoolsCallout/utility";
import { WarnWakeDialog } from "@/protoOS/components/Power";
import LoginModal from "@/protoOS/features/auth/components/LoginModal";
import { isAuthRequiredPath } from "@/protoOS/routeAuth";
import {
  useDefaultPasswordActive,
  useOnboarded,
  usePasswordSet,
  usePoolsInfo as usePoolsInfoStore,
} from "@/protoOS/store";
import {
  useAccessToken,
  useDeviceTheme,
  useFirmwareUpdateInstalling,
  useFwUpdateStatus,
  useIsMining,
  useIsMiningDriverRunning,
  useIsSleeping,
  useIsWarmingUp,
  useIsWebServerRunning,
  useMinerErrors,
  useSetDeviceTheme,
  useSetDismissedLoginModal,
  useSetShowLoginModal,
  useShowLoginModal,
  useTheme,
  useWakeDialog,
} from "@/protoOS/store";
import ErrorBoundary from "@/shared/components/ErrorBoundary";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { BootingUp } from "@/shared/components/Setup";
import { useApplyTheme } from "@/shared/features/preferences";
import { pushToast, STATUSES as TOAST_STATUSES, Toaster } from "@/shared/features/toaster";
import { useNavigate } from "@/shared/hooks/useNavigate";

interface AppProps {
  children?: ReactNode;
  fullscreen?: boolean;
  hideErrors?: boolean;
  calloutTopSpacing?: boolean;
  title: string;
  ContentLayout?: ComponentType<ContentLayoutProps>;
}

const App = ({
  children,
  fullscreen,
  hideErrors,
  calloutTopSpacing,
  title,
  ContentLayout = DefaultContentLayout,
}: AppProps) => {
  // ============================================================================
  // THEME & BOOTSTRAPPING
  // ============================================================================
  const theme = useTheme();
  const deviceTheme = useDeviceTheme();
  const setDeviceTheme = useSetDeviceTheme();

  // Apply theme effects on mount
  useApplyTheme({ theme, deviceTheme, setDeviceTheme });

  const navigate = useNavigate();
  const location = useLocation();
  const { pathname } = useMemo(() => location, [location]);

  // Infer if this is an onboarding route from the pathname
  const isOnboardingRoute = pathname.startsWith("/onboarding");
  const isPasswordChangeRoute = pathname === "/onboarding/authentication" || pathname === "/settings/authentication";

  // ============================================================================
  // STORE BOOTSTRAPPING - Fetch and populate stores
  // ============================================================================

  // Fetch system status (populates store)
  useSystemStatus();

  // Get system status from store
  // undefined = pending/not fetched yet
  const isOnboarded = useOnboarded();
  const isPasswordSet = usePasswordSet();
  const isDefaultPasswordActive = useDefaultPasswordActive();

  const { hasAccess } = useAccessToken();
  // Require defaultPasswordActive to be explicitly resolved as false before
  // firing protected hooks. `!== true` would treat `undefined` (status still
  // loading) as safe, producing a burst of 403s on a factory-password device
  // during the window between token validation and status resolution.
  const canAccessProtectedApi = hasAccess === true && isDefaultPasswordActive === false;

  // Public endpoint — runs regardless of canAccessProtectedApi so bootup
  // flags stay fresh before the user has authenticated.
  const { reload: reloadSystemInfo } = useSystemInfo({
    poll: true,
    pollIntervalMs: 35000,
  });

  const { data: miningStatus, fetchData: fetchMiningStatus } = useMiningStatus({
    enabled: canAccessProtectedApi,
    poll: true,
    pollIntervalMs: 15 * 1000,
  });

  // ============================================================================
  // ONBOARDING NAVIGATION
  // ============================================================================
  useEffect(() => {
    // Only run navigation logic after we have data from the API
    // undefined means we're still fetching
    if (isOnboarded !== undefined) {
      // Miner needs onboarding. redirect to onboarding flow
      if (!isOnboarded && !isPasswordSet && !isOnboardingRoute) {
        navigate("/onboarding/welcome");
        return;
      }

      // Device still has factory default password — redirect to authentication
      // page so the user is forced to change it before accessing anything else.
      if (isDefaultPasswordActive && !isPasswordChangeRoute) {
        navigate("/onboarding/authentication");
      }
    }
  }, [navigate, isOnboarded, isPasswordSet, isDefaultPasswordActive, isOnboardingRoute, isPasswordChangeRoute]);

  // ============================================================================
  // MINING STATUS CHECKING & WAKE LOGIC
  // ============================================================================
  const isMining = useIsMining();
  const isWarmingUp = useIsWarmingUp();
  const [initPage, setInitPage] = useState(false);
  const [intervalId, setIntervalId] = useState<ReturnType<typeof setInterval>>();
  const [wakeIntervalId, setWakeIntervalId] = useState<ReturnType<typeof setInterval>>();

  const { startMining } = useMiningStart();

  useEffect(() => {
    if (isOnboarded === false) {
      return;
    }
    if (!miningStatus) {
      fetchMiningStatus();
      // as long as the mining status is not normal, keep checking the mining status
    } else if (isMining) {
      clearInterval(intervalId);
      // eslint-disable-next-line react-hooks/set-state-in-effect -- mark init page shown once miner reaches mining state
      setInitPage(true);
      // on first load, if the device is booting up, check the mining status until it's running
    } else if (isWarmingUp && !intervalId && !initPage) {
      setInitPage(true);
      const newIntervalId = setInterval(() => {
        fetchMiningStatus();
      }, 5000);
      setIntervalId(newIntervalId);
    }
  }, [fetchMiningStatus, intervalId, initPage, miningStatus, isOnboarded, isMining, isWarmingUp]);

  const handleWake = () => {
    startMining({
      onSuccess: () => {
        const newIntervalId = setInterval(() => {
          fetchMiningStatus();
        }, 5000);
        setWakeIntervalId(newIntervalId);
      },
      onError: (error) => {
        pushToast({
          message: "Failed to wake miner. Please try again.",
          status: TOAST_STATUSES.error,
        });
        console.error("Failed to start mining:", error);
      },
    });
  };

  const afterWake = useCallback(() => {
    if (wakeIntervalId) {
      clearInterval(wakeIntervalId);
    }
  }, [wakeIntervalId]);

  // ============================================================================
  // LOGIN MODAL LOGIC
  // ============================================================================
  const showLoginModal = useShowLoginModal();
  const setShowLoginModal = useSetShowLoginModal();
  const setDismissedLoginModal = useSetDismissedLoginModal();

  const handleDismissLogin = useCallback(() => {
    // Every auth-required route is render-gated when canAccessProtectedApi is
    // false, so dismissing from "/" or any settings page would leave the user
    // staring at a blank screen. Route them to /settings/authentication — the
    // public password-reset entry point — so they have a reachable recovery
    // path. Non-auth-required pages (onboarding, authentication) stay put.
    if (isAuthRequiredPath(pathname)) {
      navigate("/settings/authentication");
    }
    setDismissedLoginModal(true);
  }, [navigate, pathname, setDismissedLoginModal]);

  const handleSuccessLogin = useCallback(() => {
    setShowLoginModal(false);
    pushToast({
      message: "You are now logged in as admin",
      status: TOAST_STATUSES.success,
    });
  }, [setShowLoginModal]);

  // ============================================================================
  // MINER STATE FOR CALLOUTS
  // ============================================================================
  const isSleeping = useIsSleeping();
  const errors = useMinerErrors();
  const wakeDialog = useWakeDialog();
  const poolsInfo = usePoolsInfoStore();

  // Suppress the global "No mining pools configured" banner on the Pools
  // page when it's already in its own null state — otherwise the user sees
  // the same empty-state message and CTA twice.
  const { arePoolsConfigured, shouldShowNoPoolsCallout } = useMemo(
    () => getNoPoolsCalloutState(poolsInfo, pathname),
    [poolsInfo, pathname],
  );

  const hasVisibleCallout = isWarmingUp || isSleeping || shouldShowNoPoolsCallout;

  // ============================================================================
  // DERIVED FLAGS
  // ============================================================================
  const isWebServerRunning = useIsWebServerRunning();
  const isMiningDriverRunning = useIsMiningDriverRunning();

  // ============================================================================
  // FIRMWARE UPDATE AUTO-REFRESH AFTER REBOOT
  // ============================================================================
  // Track firmware update progress to force browser refresh when version changes
  // Using refs to avoid circular dependencies and unnecessary re-renders
  const preUpdateVersionRef = useRef<string | undefined>(undefined);
  const hasStartedTrackingRef = useRef<boolean>(false);
  const minerWasOfflineRef = useRef<boolean>(false);

  const isUpdateInProgress = useFirmwareUpdateInstalling();
  const fwUpdateStatus = useFwUpdateStatus();
  const currentVersion = fwUpdateStatus?.current_version;

  // Detect when firmware update enters an in-progress state
  // (downloading, downloaded, installing, or confirming)
  useEffect(() => {
    // Start tracking when update begins and we haven't started tracking yet
    if (isUpdateInProgress && !hasStartedTrackingRef.current && currentVersion) {
      hasStartedTrackingRef.current = true;
      preUpdateVersionRef.current = currentVersion;
      minerWasOfflineRef.current = false;
    }

    // Reset tracking when update completes (no longer in progress)
    if (!isUpdateInProgress && hasStartedTrackingRef.current) {
      hasStartedTrackingRef.current = false;
      preUpdateVersionRef.current = undefined;
      minerWasOfflineRef.current = false;
    }
  }, [isUpdateInProgress, currentVersion]);

  // Monitor web server status to detect reboot completion and version change
  useEffect(() => {
    if (!hasStartedTrackingRef.current) return;

    // Track when miner goes offline during update
    if (!isWebServerRunning && !minerWasOfflineRef.current) {
      minerWasOfflineRef.current = true;
      return;
    }

    // Miner came back online after being offline during update
    if (isWebServerRunning && minerWasOfflineRef.current) {
      const preUpdateVersion = preUpdateVersionRef.current;

      // Wait for version data to be available before deciding
      if (!currentVersion) {
        // Version data not yet available, keep waiting
        return;
      }

      // If version changed, force browser refresh to load new UI assets
      if (preUpdateVersion && currentVersion !== preUpdateVersion) {
        // Force browser refresh to load new firmware UI assets
        window.location.reload();
      } else {
        // No version change detected, reset tracking
        hasStartedTrackingRef.current = false;
        preUpdateVersionRef.current = undefined;
        minerWasOfflineRef.current = false;
      }
    }
  }, [isWebServerRunning, currentVersion]);

  // ============================================================================
  // LOADING STATES
  // ============================================================================
  // Skip the mining driver check during onboarding since the miner may not be fully operational yet
  if (!isWebServerRunning || (!isOnboardingRoute && !isMiningDriverRunning)) {
    return <BootingUp />;
  }

  // Show loading spinner while waiting for system status
  // undefined = still fetching
  if (isOnboarded === undefined) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  // Prevent flash of app UI before redirecting to onboarding
  // If user needs onboarding and is NOT on an onboarding route, show loading
  if (!isOnboarded && !isPasswordSet && !isOnboardingRoute) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  // Block child render while the factory password is still active and we're
  // not already on a password-change route. Without this, page-level hooks
  // that aren't threaded through canAccessProtectedApi (useTelemetry etc.)
  // would mount and fire a 403 burst before the redirect effect navigates.
  if (isDefaultPasswordActive && !isPasswordChangeRoute) {
    return <BootingUp />;
  }

  // Auth-required routes must not mount their page-level hooks until both
  // hasAccess and defaultPasswordActive have resolved favorably — otherwise
  // route hooks (useTelemetry, useTimeSeries, etc.) fire 401/403 bursts before
  // LoginModal or the default-password redirect takes over. LoginModal itself
  // still renders below so the user can recover from an expired session.
  const gateRouteChildren = isAuthRequiredPath(pathname) && !canAccessProtectedApi;

  // ============================================================================
  // RENDER
  // ============================================================================
  return (
    <ErrorBoundary>
      {canAccessProtectedApi ? <AuthenticatedShell reloadSystemInfo={reloadSystemInfo} /> : null}

      {/* Toaster - Fixed position, renders above everything */}
      <div className="fixed right-4 bottom-4 z-10 phone:right-2 phone:bottom-2">
        <Toaster />
      </div>

      {/* Login Modal - Layout agnostic */}
      {showLoginModal ? <LoginModal onDismiss={handleDismissLogin} onSuccess={handleSuccessLogin} /> : null}

      {/* Wake Dialog - Layout agnostic */}
      <WarnWakeDialog open={wakeDialog.show} onClose={wakeDialog.onClose} onSubmit={wakeDialog.onConfirm} />

      <Suspense
        fallback={
          <div className="flex min-h-screen items-center justify-center">
            <ProgressCircular indeterminate />
          </div>
        }
      >
        {gateRouteChildren ? null : fullscreen ? (
          // Fullscreen mode: Just render children without AppLayout chrome
          children
        ) : (
          // Normal mode: Render with AppLayout + callouts
          <AppLayout title={title} ContentLayout={ContentLayout} type={navigationMenuTypes.app}>
            {calloutTopSpacing && hasVisibleCallout ? <div className="pt-14 phone:pt-6 tablet:pt-6" /> : null}
            {isWarmingUp ? <WarmingUpCallout /> : <WakeCallout afterWake={afterWake} onWake={handleWake} />}
            {shouldShowNoPoolsCallout && !isWarmingUp ? (
              <NoPoolsCallout arePoolsConfigured={arePoolsConfigured} />
            ) : null}
            {!isWarmingUp && !isSleeping && errors.errors?.length && !hideErrors ? <ErrorCallout /> : null}
            {children}
          </AppLayout>
        )}
      </Suspense>
    </ErrorBoundary>
  );
};

export default App;
