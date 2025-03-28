import { useEffect, useState } from "react";
import { useMiningStatus } from "@/protoOS/api";
import { GetAsicTemperatureParams } from "@/protoOS/api/types";

// Sets granularity based on mining uptime
const useGranularity = () => {
  const { data: miningStatus } = useMiningStatus({ poll: true });
  const [granularity, setGranularity] =
    useState<GetAsicTemperatureParams["granularity"]>("1m");

  useEffect(() => {
    const rebootUptimeInSeconds = miningStatus?.reboot_uptime_s;
    const miningUptimeInSeconds = miningStatus?.mining_uptime_s;
    if (
      rebootUptimeInSeconds === undefined &&
      miningUptimeInSeconds === undefined
    )
      return;

    let uptimeInSeconds = 0;
    if (
      rebootUptimeInSeconds !== undefined &&
      miningUptimeInSeconds !== undefined
    ) {
      uptimeInSeconds = Math.min(rebootUptimeInSeconds, miningUptimeInSeconds);
    } else if (rebootUptimeInSeconds !== undefined) {
      uptimeInSeconds = rebootUptimeInSeconds;
    } else if (miningUptimeInSeconds !== undefined) {
      uptimeInSeconds = miningUptimeInSeconds;
    }

    const oneHourInSeconds = 60 * 60;
    const sixHoursInSeconds = oneHourInSeconds * 6;
    if (uptimeInSeconds > sixHoursInSeconds) {
      setGranularity("15m");
    } else if (uptimeInSeconds > oneHourInSeconds) {
      setGranularity("5m");
    } else {
      setGranularity("1m");
    }
  }, [miningStatus]);

  return granularity;
};

export default useGranularity;
