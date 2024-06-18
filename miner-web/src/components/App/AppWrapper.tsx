import { ReactNode, useCallback, useContext, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import {
  ApiContext,
  useMiningStart,
  useMiningStatus,
  useSystemInfo,
} from "api";

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
  const { startMining } = useMiningStart();
  const { data: systemInfo, pending: pendingSystemInfo } = useSystemInfo();
  const navigate = useNavigate();

  // navigate to onboarding page if miner has not been onboarded
  useEffect(() => {
    if (systemInfo && "onboarded" in systemInfo && !systemInfo.onboarded) {
      navigate("/onboarding");
    }
  }, [systemInfo, navigate]);

  useEffect(() => {
    if (!miningStatus) {
      getMiningStatus();
    } else if (!intervalId) {
      // on first load, if the device is booting up, check the mining status until it's running
      // TODO: replace this with a warming up message when API tells us if device is booting up
      if (miningStatus?.status === "Stopped") {
        const newIntervalId = setInterval(() => {
          getMiningStatus({ onSuccess: setMiningStatus });
        }, 30000);
        setIntervalId(newIntervalId);
      }
    } else if (miningStatus?.status === "Running") {
      clearInterval(intervalId);
    }
  }, [getMiningStatus, setMiningStatus, intervalId, miningStatus]);

  const handleWake = () => {
    startMining();
    const newIntervalId = setInterval(() => {
      getMiningStatus({ onSuccess: setMiningStatus });
    }, 10000);
    setWakeIntervalId(newIntervalId);
  };

  const afterWake = useCallback(() => {
    if (wakeIntervalId) {
      clearInterval(wakeIntervalId);
    }
  }, [wakeIntervalId]);

  return (
    <>
      {pendingSystemInfo && !systemInfo ? (
        <div className="min-h-screen flex items-center justify-center">
          <Spinner />
        </div>
      ) : (
        <App
          title={title}
          apiMiningStatus={miningStatus}
          onWake={handleWake}
          afterWake={afterWake}
        >
          {children}
        </App>
      )}
    </>
  );
};

export default AppWrapper;
