import { useCallback, useState } from "react";

import PowerWidget from "./PowerWidget";
import {
  useMiningStart,
  useMiningStatus,
  useMiningStop,
  useSystemReboot,
} from "@/protoOS/api";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext";
import { PopoverProvider } from "@/shared/components/Popover";

interface PowerWidgetWrapperProps {
  shouldShowPopover?: boolean;
}

const PowerWidgetWrapper = ({ shouldShowPopover }: PowerWidgetWrapperProps) => {
  const { rebootSystem } = useSystemReboot();
  const [rebootSystemError, setRebootSystemError] = useState<ErrorProps>();
  const { stopMining } = useMiningStop();
  const [stopMiningError, setStopMiningError] = useState<ErrorProps>();
  const { startMining } = useMiningStart();
  const [startMiningError, setStartMiningError] = useState<ErrorProps>();
  const { miningStatus, setMiningStatus } = useMinerStatus();
  const { fetchData: fetchMiningStatus } = useMiningStatus({ poll: false });

  const [intervalId, setIntervalId] =
    useState<ReturnType<typeof setInterval>>();
  const [isMiningStatusStale, setIsMiningStatusStale] = useState(false);

  const pollMiningStatus = useCallback(
    (timeout: number) => {
      const newIntervalId = setInterval(() => {
        fetchMiningStatus({
          onSuccess: (newMiningStatus) => {
            setIsMiningStatusStale(false);
            setMiningStatus(newMiningStatus);
          },
        });
      }, timeout);
      setIntervalId(newIntervalId);
    },
    [fetchMiningStatus, setMiningStatus],
  );

  const reboot = () => {
    setRebootSystemError(undefined);
    setIsMiningStatusStale(true);
    rebootSystem({
      onError: (error) => {
        setRebootSystemError(error);
        setIsMiningStatusStale(false);
      },
      onSuccess: () => {
        pollMiningStatus(5000);
      },
    });
  };

  const handleReboot = () => {
    reboot();
  };

  const handleSleep = () => {
    setStopMiningError(undefined);
    setIsMiningStatusStale(true);
    stopMining({
      onError: (error) => {
        setStopMiningError(error);
        setIsMiningStatusStale(false);
      },
      onSuccess: () => {
        pollMiningStatus(2500);
      },
    });
  };

  const handleWake = () => {
    setStartMiningError(undefined);
    setIsMiningStatusStale(true);
    startMining({
      onError: (error) => {
        setStartMiningError(error);
        setIsMiningStatusStale(false);
      },
      onSuccess: () => {
        pollMiningStatus(5000);
      },
    });
  };

  const handleClear = () => {
    clearInterval(intervalId);
  };

  return (
    <PopoverProvider>
      <PowerWidget
        miningStatus={isMiningStatusStale ? {} : miningStatus}
        onReboot={handleReboot}
        rebootError={rebootSystemError}
        onSleep={handleSleep}
        sleepError={stopMiningError}
        onWake={handleWake}
        wakeError={startMiningError}
        afterSleep={handleClear}
        afterWake={handleClear}
        shouldShowPopover={shouldShowPopover}
      />
    </PopoverProvider>
  );
};

export default PowerWidgetWrapper;
