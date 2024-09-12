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

import Temperature from "./Temperature";

const TemperatureWrapper = () => {
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
  const { data: miningStatus, fetchData: fetchMiningStatus } =
    useMiningStatus();

  usePoll({
    fetchData: fetchMiningStatus,
    poll: true,
  });

  return (
    <Temperature
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

export default TemperatureWrapper;
