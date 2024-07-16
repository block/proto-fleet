import { useCallback, useRef, useState } from "react";

import {
  useMiningStart,
  useMiningStatus,
  useMiningStop,
  useSystemLogs,
  useSystemReboot,
} from "api";
import { LogsResponseLogs } from "apiTypes";

import { useApiContext } from "common/hooks/useApiContext";

import {
  formatLogs,
  formatLogType,
  getExportLink,
} from "pages/MinerLogs/utility";

import PowerWidget from "./PowerWidget";

interface PowerWidgetWrapperProps {
  shouldShowPopover?: boolean;
}

const PowerWidgetWrapper = ({ shouldShowPopover }: PowerWidgetWrapperProps) => {
  const { rebootSystem } = useSystemReboot();
  const { stopMining } = useMiningStop();
  const { startMining } = useMiningStart();
  const { miningStatus, setMiningStatus } = useApiContext();
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
    [fetchMiningStatus, setMiningStatus]
  );

  const reboot = () => {
    rebootSystem();
    pollMiningStatus(5000);
    setIsMiningStatusStale(true);
  };

  const downloadLogs = (logsData?: LogsResponseLogs) => {
    if (logsData?.content?.length) {
      const formattedLogs = formatLogs(logsData.content);
      const exportLink = getExportLink([
        "Time,Type,Message",
        ...formattedLogs.map(
          (log) =>
            `${log.timestamp},${formatLogType(log.logType)},${log.message.replace(/,/g, " | ")}`
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
      linkRef={linkRef}
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
