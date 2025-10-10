import {
  ComponentType,
  ReactNode,
  useCallback,
  useEffect,
  useState,
} from "react";

import App from "./App";
import { isMining, isWarmingUp } from "./utility";
import {
  useErrors,
  useHardware,
  useHashboardStatus,
  useMiningStart,
  useMiningStatus,
  usePoll,
  useSystemStatus,
} from "@/protoOS/api";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import DefaultContentLayout from "@/protoOS/components/ContentLayout/DefaultContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext";
import { MinerStatusProvider } from "@/protoOS/contexts/MinerStatusContext";
import { useSystemContext } from "@/protoOS/contexts/SystemContext";
import { FirmwareUpdateProvider } from "@/protoOS/features/firmwareUpdate/contexts/FirmwareUpdateContext";
import {
  useDeviceTheme,
  useHashboardSerials,
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

  const { setMiningStatus } = useMinerStatus();
  const [initPage, setInitPage] = useState(false);
  const {
    data: errors,
    fetchData: fetchErrors,
    pending: pendingErrors,
  } = useErrors();
  const { data: miningStatus, fetchData: fetchMiningStatus } = useMiningStatus({
    poll: false,
  });
  const [intervalId, setIntervalId] =
    useState<ReturnType<typeof setInterval>>();
  const [wakeIntervalId, setWakeIntervalId] =
    useState<ReturnType<typeof setInterval>>();
  const { data: systemStatus, pending: pendingSystemStatus } =
    useSystemStatus();
  const { startMining } = useMiningStart();
  const [startMiningError, setStartMiningError] = useState<ErrorProps>();
  const {
    data: systemInfo,
    processedData: processedSystemInfo,
    pending: pendingSystemInfo,
  } = useSystemContext();
  const { getItem, setItem } = useLocalStorage();
  const navigate = useNavigate();

  // Fetch and populate hardware store
  useHardware();

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

  usePoll({
    fetchData: fetchErrors,
    poll: true,
    pollIntervalMs: 10000,
  });

  useEffect(() => {
    if (!systemStatus?.onboarded) {
      return;
    }
    if (!miningStatus) {
      fetchMiningStatus();
      // as long as the mining status is not normal, keep checking the mining status
    } else if (isMining(miningStatus?.status)) {
      clearInterval(intervalId);
      setInitPage(true);
      // on first load, if the device is booting up, check the mining status until it's running
    } else if (isWarmingUp(miningStatus) && !intervalId && !initPage) {
      setInitPage(true);
      const newIntervalId = setInterval(() => {
        fetchMiningStatus({ onSuccess: setMiningStatus });
      }, 5000);
      setIntervalId(newIntervalId);
    }
  }, [
    fetchMiningStatus,
    setMiningStatus,
    intervalId,
    initPage,
    miningStatus,
    systemStatus,
  ]);

  const handleWake = () => {
    setStartMiningError(undefined);
    startMining({
      onError: setStartMiningError,
      onSuccess: () => {
        const newIntervalId = setInterval(() => {
          fetchMiningStatus({ onSuccess: setMiningStatus });
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
        if (
          (pendingSystemInfo && processedSystemInfo === undefined) ||
          !processedSystemInfo?.isWebServerRunning ||
          !processedSystemInfo.isMiningDriverRunning
        ) {
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
          <MinerStatusProvider
            apiErrors={errors}
            apiMiningStatus={miningStatus}
            pendingErrors={pendingErrors}
          >
            <FirmwareUpdateProvider systemInfo={systemInfo}>
              {/* TODO: [STORE_REFACTOR] 
                Once we add miner status, system info etc to global store, we should
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
                  systemInfo={systemInfo}
                  pendingSystemInfo={pendingSystemInfo}
                  hideErrors={hideErrors}
                  ContentLayout={ContentLayout}
                >
                  {children}
                </App>
              )}
            </FirmwareUpdateProvider>
          </MinerStatusProvider>
        );
      })()}
    </ErrorBoundary>
  );
};

export default AppWrapper;
