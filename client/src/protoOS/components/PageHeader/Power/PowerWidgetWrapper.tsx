import { useCallback, useState } from "react";

import PowerWidget from "./PowerWidget";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { useMiningStatus } from "@/protoOS/api/hooks/useMiningStatus";
import { useMiningStop } from "@/protoOS/api/hooks/useMiningStop";
import { useSystemReboot } from "@/protoOS/api/hooks/useSystemReboot";
import { WakingDialog } from "@/protoOS/components/Power";
import { useWakeMiner } from "@/protoOS/hooks/useWakeMiner";
import { useSetMiningStatus } from "@/protoOS/store";
import { PopoverProvider } from "@/shared/components/Popover";

interface PowerWidgetWrapperProps {
  shouldShowPopover?: boolean;
}

const PowerWidgetWrapper = ({ shouldShowPopover }: PowerWidgetWrapperProps) => {
  const { rebootSystem } = useSystemReboot();
  const [rebootSystemError, setRebootSystemError] = useState<ErrorProps>();
  const { stopMining } = useMiningStop();
  const [stopMiningError, setStopMiningError] = useState<ErrorProps>();
  const setMiningStatus = useSetMiningStatus();
  const { fetchData: fetchMiningStatus } = useMiningStatus({ poll: false });

  const [intervalId, setIntervalId] = useState<ReturnType<typeof setInterval>>();

  const handleClear = useCallback(() => {
    clearInterval(intervalId);
  }, [intervalId]);

  const pollMiningStatus = useCallback(
    (timeout: number) => {
      const newIntervalId = setInterval(() => {
        fetchMiningStatus({
          onSuccess: (newMiningStatus) => {
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
    afterWake: handleClear,
  });

  const reboot = () => {
    setRebootSystemError(undefined);
    rebootSystem({
      onError: (error) => {
        setRebootSystemError(error);
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
    stopMining({
      onError: (error) => {
        setStopMiningError(error);
      },
      onSuccess: () => {
        pollMiningStatus(2500);
      },
    });
  };

  return (
    <PopoverProvider>
      <PowerWidget
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
      <WakingDialog open={shouldWake} />
    </PopoverProvider>
  );
};

export default PowerWidgetWrapper;
