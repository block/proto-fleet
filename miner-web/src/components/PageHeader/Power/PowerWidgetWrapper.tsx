import { useCallback, useContext, useState } from "react";

import {
  ApiContext,
  useMiningStart,
  useMiningStatus,
  useMiningStop,
  useSystemReboot,
} from "api";

import PowerWidget from "./PowerWidget";

interface PowerWidgetWrapperProps {
  shouldShowPopover?: boolean;
}

const PowerWidgetWrapper = ({ shouldShowPopover }: PowerWidgetWrapperProps) => {
  const { rebootSystem } = useSystemReboot();
  const { stopMining } = useMiningStop();
  const { startMining } = useMiningStart();
  const { miningStatus, setMiningStatus } = useContext(ApiContext);
  const { getMiningStatus } = useMiningStatus();

  const [intervalId, setIntervalId] =
    useState<ReturnType<typeof setInterval>>();
  const [isMiningStatusStale, setIsMiningStatusStale] = useState(false);

  const pollMiningStatus = useCallback(
    (timeout: number) => {
      const newIntervalId = setInterval(() => {
        getMiningStatus({
          onSuccess: (newMiningStatus) => {
            setIsMiningStatusStale(false);
            setMiningStatus(newMiningStatus);
          },
        });
      }, timeout);
      setIntervalId(newIntervalId);
    },
    [getMiningStatus, setMiningStatus]
  );

  const handleReboot = () => {
    rebootSystem();
    pollMiningStatus(5000);
    setIsMiningStatusStale(true);
  };

  const handleSleep = () => {
    stopMining();
    pollMiningStatus(2500);
    setIsMiningStatusStale(true);
  };

  const handleWake = () => {
    startMining();
    pollMiningStatus(5000);
    setIsMiningStatusStale(true);
  };

  const handleClear = () => {
    clearInterval(intervalId);
  };

  return (
    <PowerWidget
      miningStatus={isMiningStatusStale ? {} : miningStatus}
      onReboot={handleReboot}
      onSleep={handleSleep}
      onWake={handleWake}
      afterReboot={handleClear}
      afterSleep={handleClear}
      afterWake={handleClear}
      shouldShowPopover={shouldShowPopover}
    />
  );
};

export default PowerWidgetWrapper;
