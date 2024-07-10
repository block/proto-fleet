import { useState } from "react";

import {
  useCoolingStatus,
  useHashboards,
  useMiningStatus,
  usePoll,
  useTemperature,
} from "api";

import { useLocalStorage } from "common/hooks/useLocalStorage";

import { Duration, durations } from "components/DurationSelector";

import Hardware from "./Hardware";

const HardwareWrapper = () => {
  const { getItem } = useLocalStorage();
  const [duration, setDuration] = useState<Duration>(
    getItem("duration") || durations[0]
  );
  const { data: tempData, pending: pendingTempData } = useTemperature({
    duration,
    poll: true,
  });
  const { data: hashboardsInfo, pending: pendingHashboardsInfo } =
    useHashboards();
  const { data: coolingStatus, pending: pendingCoolingStatus } =
    useCoolingStatus({ poll: true });
  const {
    data: miningStatus,
    fetchData: fetchMiningStatus,
    pending: pendingMiningStatus,
  } = useMiningStatus();

  usePoll({
    data: miningStatus,
    fetchData: fetchMiningStatus,
    pending: pendingMiningStatus,
    poll: true,
  });

  return (
    <Hardware
      coolingStatus={coolingStatus}
      duration={duration}
      hashboardsInfo={hashboardsInfo}
      miningStatus={miningStatus}
      pendingCoolingStatus={pendingCoolingStatus}
      pendingHashboardsInfo={pendingHashboardsInfo}
      pendingTempData={pendingTempData}
      setDuration={setDuration}
      tempData={tempData}
    />
  );
};

export default HardwareWrapper;
