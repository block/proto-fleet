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
  useMiningStart,
  useMiningStatus,
  usePoll,
  useSystemInfo,
  useSystemStatus,
} from "@/protoOS/api";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import DefaultContentLayout from "@/protoOS/components/ContentLayout/DefaultContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext";
import { MinerStatusProvider } from "@/protoOS/contexts/MinerStatusContext";
import { FirmwareUpdateProvider } from "@/protoOS/features/firmwareUpdate/contexts/FirmwareUpdateContext";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { BootingUp } from "@/shared/components/Setup";
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
  title,
  ContentLayout = DefaultContentLayout,
}: AppProps) => {
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
  } = useSystemInfo({ poll: true });
  const { getItem, setItem } = useLocalStorage();
  const navigate = useNavigate();

  // navigate to onboarding page if miner has not been onboarded
  useEffect(() => {
    if (!pendingSystemStatus && systemStatus?.onboarded !== undefined) {
      if (!systemStatus.onboarded) {
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
    <>
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
            </FirmwareUpdateProvider>
          </MinerStatusProvider>
        );
      })()}
    </>
  );
};

export default AppWrapper;
