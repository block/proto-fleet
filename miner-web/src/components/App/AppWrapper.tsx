import { ReactNode, useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import {
  useErrors,
  useMiningStart,
  useMiningStatus,
  usePoll,
  useSystemInfo,
  useSystemStatus,
} from "api";

import { useApiContext } from "common/hooks/useApiContext";
import { useLocalStorage } from "common/hooks/useLocalStorage";

import Spinner from "components/Spinner";

import App from "./App";
import { isMining, isWarmingUp } from "./utility";

interface AppProps {
  children?: ReactNode;
  fullScreen?: boolean;
  hideErrors?: boolean;
  title: string;
}

const AppWrapper = ({ children, fullScreen, hideErrors, title }: AppProps) => {
  const { setMiningStatus } = useApiContext();
  const [initPage, setInitPage] = useState(false);
  const { data: errors, fetchData: fetchErrors, pending: pendingErrors } = useErrors();
  const { data: miningStatus, fetchData: fetchMiningStatus } =
    useMiningStatus();
  const [intervalId, setIntervalId] =
    useState<ReturnType<typeof setInterval>>();
  const [wakeIntervalId, setWakeIntervalId] =
    useState<ReturnType<typeof setInterval>>();
  const { data: systemStatus, pending: pendingSystemStatus } =
    useSystemStatus();
  const { startMining } = useMiningStart();
  const { data: systemInfo, pending: pendingSystemInfo } = useSystemInfo();
  const { getItem, setItem } = useLocalStorage();
  const navigate = useNavigate();

  // navigate to onboarding page if miner has not been onboarded
  useEffect(() => {
    if (!pendingSystemStatus && systemStatus?.onboarded !== undefined) {
      if (systemStatus.onboarded) {
        setItem("isOnboarded", true);
      } else {
        setItem("isOnboarded", false);
        navigate("/onboarding");
      }
    }
  }, [navigate, setItem, systemStatus, pendingSystemStatus]);

  usePoll({
    fetchData: fetchErrors,
    poll: true,
    pollIntervalMs: 5000,
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
    startMining();
    const newIntervalId = setInterval(() => {
      fetchMiningStatus({ onSuccess: setMiningStatus });
    }, 5000);
    setWakeIntervalId(newIntervalId);
  };

  const afterWake = useCallback(() => {
    if (wakeIntervalId) {
      clearInterval(wakeIntervalId);
    }
  }, [wakeIntervalId]);

  return (
    <>
      {!getItem("isOnboarded") &&
      pendingSystemStatus &&
      systemStatus?.onboarded === undefined ? (
        <div className="min-h-screen flex items-center justify-center">
          <Spinner />
        </div>
      ) : (
        <App
          title={title}
          apiErrors={errors}
          pendingErrors={pendingErrors}
          apiMiningStatus={miningStatus}
          onWake={handleWake}
          afterWake={afterWake}
          systemInfo={systemInfo}
          pendingSystemInfo={pendingSystemInfo}
          fullScreen={fullScreen}
          hideErrors={hideErrors}
        >
          {children}
        </App>
      )}
    </>
  );
};

export default AppWrapper;
