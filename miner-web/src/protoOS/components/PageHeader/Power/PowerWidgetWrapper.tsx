import { useCallback, useRef, useState } from "react";

import PowerWidget from "./PowerWidget";
import {
  useMiningStart,
  useMiningStatus,
  useMiningStop,
  useSystemLogs,
  useSystemReboot,
} from "@/protoOS/api";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { LogsResponseLogs } from "@/protoOS/api/types";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext";
import {
  formatLogs,
  formatLogType,
  getExportLink,
} from "@/protoOS/pages/MinerLogs/utility";

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
  const { fetchData: fetchMiningStatus } = useMiningStatus();
  const { fetchData: fetchLogs } = useSystemLogs();
  const linkRef = useRef<HTMLAnchorElement>(null);

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

  const downloadLogs = (logsData?: LogsResponseLogs) => {
    if (logsData?.content?.length) {
      const formattedLogs = formatLogs(logsData.content);
      const exportLink = getExportLink([
        "Time,Type,Message",
        ...formattedLogs.map(
          (log) =>
            `${log.timestamp},${formatLogType(log.logType)},${log.message.replace(/,/g, " | ")}`,
        ),
      ]);
      linkRef.current?.setAttribute("href", exportLink);
      linkRef.current?.click();
    }
  };

  const handleReboot = async () => {
    const logsData = await fetchLogs({ lines: 10000 });
    downloadLogs(logsData);
    reboot();
  };

  // TODO: remove this when data no longer gets cleared by reload
  const handleAfterReboot = () => {
    window.location.reload();
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
    <PowerWidget
      linkRef={linkRef}
      miningStatus={isMiningStatusStale ? {} : miningStatus}
      onReboot={handleReboot}
      rebootError={rebootSystemError}
      onSleep={handleSleep}
      sleepError={stopMiningError}
      onWake={handleWake}
      wakeError={startMiningError}
      afterReboot={handleAfterReboot}
      afterSleep={handleClear}
      afterWake={handleClear}
      shouldShowPopover={shouldShowPopover}
    />
  );
};

export default PowerWidgetWrapper;
