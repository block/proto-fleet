import {
  ComponentType,
  ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import { useLocation } from "react-router-dom";

import ErrorCallout from "./ErrorCallout";
import WakeCallout from "./WakeCallout";
import WarmingUpCallout from "./WarmingUpCallout";
import {
  useErrors,
  useFirmwareUpdate,
  useHardware,
  useHashboardStatus,
  useMiningStart,
  useMiningStatus,
  useNetworkInfo,
  usePoolsInfo,
  useSystemInfo,
  useSystemStatus,
} from "@/protoOS/api";
import AppLayout from "@/protoOS/components/AppLayout";
import DefaultContentLayout from "@/protoOS/components/ContentLayout/DefaultContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import { navigationMenuTypes } from "@/protoOS/components/NavigationMenu";
import { WarnWakeDialog } from "@/protoOS/components/Power";
import LoginModal from "@/protoOS/features/auth/components/LoginModal";
import { useOnboarded, usePasswordSet } from "@/protoOS/store";
import {
  useAccessToken,
  useComprehensiveStatus,
  useDeviceTheme,
  useHashboardSerials,
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
import {
  pushToast,
  STATUSES as TOAST_STATUSES,
  Toaster,
} from "@/shared/features/toaster";
import { useNavigate } from "@/shared/hooks/useNavigate";

interface AppProps {
  children?: ReactNode;
  fullscreen?: boolean;
  hideErrors?: boolean;
  title: string;
  ContentLayout?: ComponentType<ContentLayoutProps>;
}

const App = ({
  children,
  fullscreen,
  hideErrors,
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

  // ============================================================================
  // STORE BOOTSTRAPPING - Fetch and populate stores
  // ============================================================================

  // Fetch and populate hardware store
  useHardware();

  // Fetch and poll system info (updates store)
  const { reload: reloadSystemInfo } = useSystemInfo({
    poll: true,
    pollIntervalMs: 35000,
  });

  // Fetch network info once (updates store)
  useNetworkInfo({
    poll: false,
  });

  // Poll for errors
  useErrors({ poll: true, pollIntervalMs: 15 * 1000 });

  // Poll for mining status
  const { data: miningStatus, fetchData: fetchMiningStatus } = useMiningStatus({
    poll: true,
    pollIntervalMs: 15 * 1000,
  });

  // Poll for pools info
  usePoolsInfo({ poll: true, pollIntervalMs: 15 * 1000 });

  // Fetch system status (populates store)
  useSystemStatus();

  // Get system status from store
  // undefined = pending/not fetched yet
  const isOnboarded = useOnboarded();
  const isPasswordSet = usePasswordSet();

  // Get hashboard serials from store to fetch ASIC layout data
  const hashboardSerials = useHashboardSerials();

  // Fetch ASIC layout data for all hashboards
  // No polling needed - ASIC positions don't change
  useHashboardStatus({
    hashboardSerialNumbers: hashboardSerials,
    poll: false,
  });

  // ============================================================================
  // FIRMWARE UPDATE CHECK
  // ============================================================================
  const { checkFirmwareUpdate } = useFirmwareUpdate();
  useEffect(() => {
    const checkForFirmwareUpdates = () => {
      checkFirmwareUpdate()
        .then(() => {
          reloadSystemInfo();
        })
        .catch((error) => {
          // Check if this is a JSON parsing error we should ignore
          if (
            error?.error?.message?.includes("Unexpected end of JSON input") ||
            error?.message?.includes("Unexpected end of JSON input")
          ) {
            // JSON parsing error from empty response - this is normal, ignore it
            return;
          }
          console.error("Error checking for firmware updates:", error);
        });
    };

    // Immediately check on component mount
    checkForFirmwareUpdates();
  }, [checkFirmwareUpdate, reloadSystemInfo]);

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
      }
    }
  }, [navigate, isOnboarded, isPasswordSet, isOnboardingRoute]);

  // ============================================================================
  // MINING STATUS CHECKING & WAKE LOGIC
  // ============================================================================
  const isMining = useIsMining();
  const isWarmingUp = useIsWarmingUp();
  const [initPage, setInitPage] = useState(false);
  const [intervalId, setIntervalId] =
    useState<ReturnType<typeof setInterval>>();
  const [wakeIntervalId, setWakeIntervalId] =
    useState<ReturnType<typeof setInterval>>();

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
      setInitPage(true);
      // on first load, if the device is booting up, check the mining status until it's running
    } else if (isWarmingUp && !intervalId && !initPage) {
      setInitPage(true);
      const newIntervalId = setInterval(() => {
        fetchMiningStatus();
      }, 5000);
      setIntervalId(newIntervalId);
    }
  }, [
    fetchMiningStatus,
    intervalId,
    initPage,
    miningStatus,
    isOnboarded,
    isMining,
    isWarmingUp,
  ]);

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
    if (
      pathname === "/settings/mining-pools" ||
      pathname === "/settings/cooling"
    ) {
      // if user landed on an auth protected setting page from within the app,
      //  navigate back else navigate to home
      navigate(location.state?.from || "/");
    }
    setDismissedLoginModal(true);
  }, [navigate, pathname, setDismissedLoginModal, location]);

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
  const comprehensiveStatus = useComprehensiveStatus();
  const wakeDialog = useWakeDialog();

  // Initialize access token
  useAccessToken();

  // ============================================================================
  // DERIVED FLAGS
  // ============================================================================
  const isWebServerRunning = useIsWebServerRunning();
  const isMiningDriverRunning = useIsMiningDriverRunning();

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

  // ============================================================================
  // RENDER
  // ============================================================================
  return (
    <ErrorBoundary>
      {/* Toaster - Fixed position, renders above everything */}
      <div className="fixed right-4 bottom-4 z-10 phone:right-2 phone:bottom-2">
        <Toaster />
      </div>

      {/* Login Modal - Layout agnostic */}
      {showLoginModal && (
        <LoginModal
          onDismiss={handleDismissLogin}
          onSuccess={handleSuccessLogin}
        />
      )}

      {/* Wake Dialog - Layout agnostic */}
      <WarnWakeDialog
        onClose={wakeDialog.onClose}
        onSubmit={wakeDialog.onConfirm}
        show={wakeDialog.show}
      />

      {fullscreen ? (
        // Fullscreen mode: Just render children without AppLayout chrome
        children
      ) : (
        // Normal mode: Render with AppLayout + callouts
        <AppLayout
          title={title}
          ContentLayout={ContentLayout}
          type={navigationMenuTypes.app}
        >
          {isWarmingUp ? (
            <WarmingUpCallout />
          ) : (
            <WakeCallout afterWake={afterWake} onWake={handleWake} />
          )}
          {!isWarmingUp &&
          !isSleeping &&
          errors.errors?.length &&
          !hideErrors ? (
            <ErrorCallout status={comprehensiveStatus} />
          ) : null}
          {children}
        </AppLayout>
      )}
    </ErrorBoundary>
  );
};

export default App;
