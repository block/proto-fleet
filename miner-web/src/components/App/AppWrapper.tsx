import { ReactNode, useCallback, useContext, useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import {
  ApiContext,
  useMiningStart,
  useMiningStatus,
  useSystemInfo,
} from "api";

import { useLocalStorage } from "common/hooks/useLocalStorage";

import Spinner from "components/Spinner";

import App from "./App";

interface AppProps {
  children?: ReactNode;
  title: string;
}

const AppWrapper = ({ children, title }: AppProps) => {
  const { setMiningStatus } = useContext(ApiContext);
  const { data: miningStatus, getMiningStatus } = useMiningStatus();
  const [intervalId, setIntervalId] =
    useState<ReturnType<typeof setInterval>>();
  const [wakeIntervalId, setWakeIntervalId] =
    useState<ReturnType<typeof setInterval>>();
  const [isOnboarding, setIsOnboarding] = useState(false);
  const { startMining } = useMiningStart();
  const { data: systemInfo, pending: pendingSystemInfo } = useSystemInfo();
  const { getItem, setItem } = useLocalStorage();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();

  // navigate to onboarding page if miner has not been onboarded
  useEffect(() => {
    if (systemInfo) {
      if ("onboarded" in systemInfo && !systemInfo.onboarded) {
        setItem("isOnboarded", false);
        navigate("/onboarding");
      } else {
        setItem("isOnboarded", true);
      }
    }
  }, [systemInfo, navigate, setItem]);

  useEffect(() => {
    if (searchParams.get("onboarding")) {
      setIsOnboarding(true);
      setSearchParams("");
    }
  }, [searchParams, setSearchParams]);

  useEffect(() => {
    if (!miningStatus) {
      getMiningStatus();
    } else if (!intervalId) {
      // on first load, if the device is booting up, check the mining status until it's running
      // TODO: get this from API when booting up status available
      if (miningStatus?.status === "Stopped" && isOnboarding) {
        const newIntervalId = setInterval(() => {
          getMiningStatus({ onSuccess: setMiningStatus });
        }, 5000);
        setIntervalId(newIntervalId);
      }
    } else if (miningStatus?.status === "Running") {
      clearInterval(intervalId);
      setIsOnboarding(false);
    }
  }, [
    getMiningStatus,
    setMiningStatus,
    intervalId,
    miningStatus,
    isOnboarding,
  ]);

  const handleWake = () => {
    startMining();
    const newIntervalId = setInterval(() => {
      getMiningStatus({ onSuccess: setMiningStatus });
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
      {!getItem("isOnboarded") && pendingSystemInfo && !systemInfo ? (
        <div className="min-h-screen flex items-center justify-center">
          <Spinner />
        </div>
      ) : (
        <App
          title={title}
          apiMiningStatus={miningStatus}
          onWake={handleWake}
          afterWake={afterWake}
          isOnboarding={isOnboarding}
        >
          {children}
        </App>
      )}
    </>
  );
};

export default AppWrapper;
