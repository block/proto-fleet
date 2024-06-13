import { ReactNode, useCallback, useContext, useEffect, useState } from "react";

import { ApiContext, useMiningStart, useMiningStatus } from "api";

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
  const { startMining } = useMiningStart();

  useEffect(() => {
    getMiningStatus();
  }, [getMiningStatus]);

  const handleWake = () => {
    startMining();
    const newIntervalId = setInterval(() => {
      getMiningStatus({ onSuccess: setMiningStatus });
    }, 10000);
    setIntervalId(newIntervalId);
  };

  const afterWake = useCallback(() => {
    if (intervalId) {
      clearInterval(intervalId);
    }
  }, [intervalId]);

  return (
    <App
      title={title}
      apiMiningStatus={miningStatus}
      onWake={handleWake}
      afterWake={afterWake}
    >
      {children}
    </App>
  );
};

export default AppWrapper;
