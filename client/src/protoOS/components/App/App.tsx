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
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";
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

  const { getItem, setItem } = useLocalStorage();
  const navigate = useNavigate();
  const location = useLocation();
  const { pathname } = useMemo(() => location, [location]);

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

  // Get system status
  const { data: systemStatus, pending: pendingSystemStatus } =
    useSystemStatus();

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
    if (!pendingSystemStatus && systemStatus?.onboarded !== undefined) {
      if (!systemStatus.onboarded && !systemStatus.password_set) {
        setItem("isOnboarded", false);
        navigate("/onboarding/welcome");
      } else {
        setItem("isOnboarded", true);
      }
    }
  }, [navigate, setItem, systemStatus, pendingSystemStatus]);

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
    if (!systemStatus?.onboarded) {
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
    systemStatus,
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
  if (!isWebServerRunning || !isMiningDriverRunning) {
    return <BootingUp />;
  }

  if (
    !getItem("isOnboarded") &&
    pendingSystemStatus &&
    systemStatus?.onboarded === undefined
  ) {
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
