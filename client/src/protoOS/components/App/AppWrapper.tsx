import {
  ComponentType,
  ReactNode,
  useCallback,
  useEffect,
  useState,
} from "react";

import App from "./App";
import {
  useErrors,
  useFirmwareUpdate,
  useHardware,
  useHashboardStatus,
  useMiningStart,
  useMiningStatus,
  usePoolsInfo,
  useSystemInfo,
  useSystemStatus,
} from "@/protoOS/api";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import DefaultContentLayout from "@/protoOS/components/ContentLayout/DefaultContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import { useIsMining, useIsWarmingUp } from "@/protoOS/store";
import {
  useDeviceTheme,
  useHashboardSerials,
  useIsMiningDriverRunning,
  useIsWebServerRunning,
  useSetDeviceTheme,
  useTheme,
} from "@/protoOS/store";
import ErrorBoundary from "@/shared/components/ErrorBoundary";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { BootingUp } from "@/shared/components/Setup";
import { useApplyTheme } from "@/shared/features/preferences";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";
import { useNavigate } from "@/shared/hooks/useNavigate";

interface AppProps {
  children?: ReactNode;
  fullScreen?: boolean;
  hideErrors?: boolean;
  title: string;
  ContentLayout?: ComponentType<ContentLayoutProps>;
}

const AppWrapper = ({
  children,
  hideErrors,
  fullScreen,
  title,
  ContentLayout = DefaultContentLayout,
}: AppProps) => {
  const theme = useTheme();
  const deviceTheme = useDeviceTheme();
  const setDeviceTheme = useSetDeviceTheme();

  // Apply theme effects on mount
  useApplyTheme({ theme, deviceTheme, setDeviceTheme });

  const isMining = useIsMining();
  const isWarmingUp = useIsWarmingUp();
  const [initPage, setInitPage] = useState(false);
  useErrors({ poll: true, pollIntervalMs: 15 * 1000 });
  const { data: miningStatus, fetchData: fetchMiningStatus } = useMiningStatus({
    poll: true,
    pollIntervalMs: 15 * 1000,
  });
  usePoolsInfo({ poll: true, pollIntervalMs: 15 * 1000 });
  const [intervalId, setIntervalId] =
    useState<ReturnType<typeof setInterval>>();
  const [wakeIntervalId, setWakeIntervalId] =
    useState<ReturnType<typeof setInterval>>();
  const { data: systemStatus, pending: pendingSystemStatus } =
    useSystemStatus();
  const { startMining } = useMiningStart();
  const [startMiningError, setStartMiningError] = useState<ErrorProps>();

  // Derived flags from store
  const isWebServerRunning = useIsWebServerRunning();
  const isMiningDriverRunning = useIsMiningDriverRunning();

  const { getItem, setItem } = useLocalStorage();
  const navigate = useNavigate();

  // Fetch and populate hardware store
  useHardware();

  // Fetch and poll system info (updates store)
  const { reload: reloadSystemInfo } = useSystemInfo({
    poll: true,
    pollIntervalMs: 35000,
  });

  // Check for firmware updates on mount
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

  // TODO: (STORE_REFACTOR) get fw to add more data in this EP
  // Currently useHardware gets us most of the data we need to populate the hardware slice
  // But we are missing data around asics that currently only comes from the hashboard status EP
  // - add hashboard.asics - each asic should have index, row, column
  // - add hasbboard.bayIndex
  // - add mine-info to response - miner infro should include bayCount

  // Get hashboard serials from store to fetch ASIC layout data
  const hashboardSerials = useHashboardSerials();

  // Fetch ASIC layout data for all hashboards
  // No polling needed - ASIC positions don't change
  useHashboardStatus({
    hashboardSerialNumbers: hashboardSerials,
    poll: false,
  });

  // navigate to onboarding page if miner has not been onboarded
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
    setStartMiningError(undefined);
    startMining({
      onError: setStartMiningError,
      onSuccess: () => {
        const newIntervalId = setInterval(() => {
          fetchMiningStatus();
        }, 5000);
        setWakeIntervalId(newIntervalId);
      },
    });
  };

  const afterWake = useCallback(() => {
    if (wakeIntervalId) {
      clearInterval(wakeIntervalId);
    }
  }, [wakeIntervalId]);

  return (
    <ErrorBoundary>
      {(() => {
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

        return (
          <>
            {/* TODO: [STORE_REFACTOR]
              Once we add system info etc to global store, we should
              be able to remove the nesting of wrapper components that comprise App.tsx

              Needed to add this conditional because full screen views were not rendering inside of App wrapper
              which is where we make calls to useHardware to populate the hardware slice
            */}
            {fullScreen ? (
              <ContentLayout />
            ) : (
              <App
                title={title}
                onWake={handleWake}
                wakeError={startMiningError}
                afterWake={afterWake}
                hideErrors={hideErrors}
                ContentLayout={ContentLayout}
              >
                {children}
              </App>
            )}
          </>
        );
      })()}
    </ErrorBoundary>
  );
};

export default AppWrapper;
