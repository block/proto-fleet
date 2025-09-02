import { useCallback, useState } from "react";

import PowerWidget from "./PowerWidget";
import { useMiningStatus, useMiningStop, useSystemReboot } from "@/protoOS/api";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { WakingDialog } from "@/protoOS/components/Power";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext";
import { useWakeMiner } from "@/protoOS/hooks/useWakeMiner";
import { PopoverProvider } from "@/shared/components/Popover";

interface PowerWidgetWrapperProps {
  shouldShowPopover?: boolean;
}

const PowerWidgetWrapper = ({ shouldShowPopover }: PowerWidgetWrapperProps) => {
  const { rebootSystem } = useSystemReboot();
  const [rebootSystemError, setRebootSystemError] = useState<ErrorProps>();
  const { stopMining } = useMiningStop();
  const [stopMiningError, setStopMiningError] = useState<ErrorProps>();
  const { miningStatus, setMiningStatus } = useMinerStatus();
  const { fetchData: fetchMiningStatus } = useMiningStatus({ poll: false });

  const [intervalId, setIntervalId] =
    useState<ReturnType<typeof setInterval>>();
  const [isMiningStatusStale, setIsMiningStatusStale] = useState(false);

  const handleClear = useCallback(() => {
    clearInterval(intervalId);
  }, [intervalId]);

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

  const {
    wakeMiner,
    error: wakeError,
    shouldWake,
  } = useWakeMiner({
    miningStatus,
    afterWake: handleClear,
  });

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

  return (
    <PopoverProvider>
      <PowerWidget
        miningStatus={!miningStatus || isMiningStatusStale ? {} : miningStatus}
        onReboot={handleReboot}
        rebootError={rebootSystemError}
        onSleep={handleSleep}
        sleepError={stopMiningError}
        onWake={wakeMiner}
        wakeError={wakeError}
        afterSleep={handleClear}
        afterWake={handleClear}
        shouldShowPopover={shouldShowPopover}
      />
      <WakingDialog show={shouldWake} />
    </PopoverProvider>
  );
};

export default PowerWidgetWrapper;
