import { useEffect, useMemo, useState } from "react";

import Hashrate from "./Hashrate";
import NoPoolsCallout from "./NoPoolsCallout";
import {
  useEfficiency,
  useHashboards,
  usePower,
  useTemperature,
} from "@/protoOS/api";
import { Aggregates } from "@/protoOS/api/types";

import EfficiencyWidget, {
  aggregateEfficiencyValues,
  convertEfficiencyValues,
  EfficiencyValues,
} from "@/protoOS/components/InfoWidget/EfficiencyWidget";
import PowerUsageWidget, {
  aggregatePowerValues,
  convertAggregatePowerValues,
  convertPowerValues,
} from "@/protoOS/components/InfoWidget/PowerUsageWidget";
import TempWidget from "@/protoOS/components/InfoWidget/TempWidget";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext";
import Divider from "@/shared/components/Divider";

import DurationSelector, {
  Duration,
  durations,
} from "@/shared/components/DurationSelector";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";
import { getDisplayValue } from "@/shared/utils/stringUtils";

const Home = () => {
  const { getItem, setItem } = useLocalStorage();
  const [duration, setDuration] = useState<Duration>(
    getItem("duration") || durations[0],
  );
  const [historicalEfficiency, setHistoricalEfficiency] =
    useState<EfficiencyValues>();
  const [avgEfficiency, setAvgEfficiency] = useState<string | number>();
  const [historicalPower, setHistoricalPower] =
    useState<{ datetime: number; value: string | number }[]>();
  const [powerAggregates, setPowerAggregates] = useState<Aggregates>();
  const [temp, setTemp] = useState<number>();
  const [highestTemp, setHighestTemp] = useState<number>();
  const [hashboardSerials, setHashboardSerials] = useState<string[]>();
  const { data: hashboardsInfo, pending: pendingHashboardsInfo } =
    useHashboards();
  const { data: efficiencyData, pending: pendingEfficiency } = useEfficiency({
    duration,
    poll: true,
  });
  const { data: powerData, pending: pendingPower } = usePower({
    duration,
    poll: true,
  });
  const { data: tempData, pending: pendingTempData } = useTemperature({
    duration,
    poll: true,
  });
  const { poolsInfo, poolsInfoStatus } = useMinerStatus();

  useEffect(() => {
    setHistoricalEfficiency(undefined);
    setHistoricalPower(undefined);
    setTemp(undefined);
    setItem("duration", duration);
  }, [duration, setItem]);

  useEffect(() => {
    if (hashboardsInfo) {
      setHashboardSerials(
        hashboardsInfo
          ?.map((hashboardInfo) => hashboardInfo.hb_sn)
          .filter(Boolean) as string[],
      );
    }
  }, [hashboardsInfo]);

  useEffect(() => {
    if (
      !pendingEfficiency &&
      efficiencyData?.data?.length &&
      efficiencyData.duration === duration
    ) {
      const aggregatedEfficiencyValues = aggregateEfficiencyValues(
        efficiencyData.data,
        duration,
      );
      setHistoricalEfficiency(
        convertEfficiencyValues(aggregatedEfficiencyValues),
      );
      setAvgEfficiency(getDisplayValue(efficiencyData.aggregates?.avg));
    }
  }, [efficiencyData, pendingEfficiency, duration]);

  useEffect(() => {
    if (
      !pendingPower &&
      powerData?.data?.length &&
      powerData.duration === duration
    ) {
      const aggregatedPowerValues = aggregatePowerValues(
        powerData.data,
        duration,
      );
      setHistoricalPower(convertPowerValues(aggregatedPowerValues));
      setPowerAggregates(convertAggregatePowerValues(powerData.aggregates));
    }
  }, [duration, pendingPower, powerData]);

  useEffect(() => {
    if (
      !pendingTempData &&
      tempData?.data?.length &&
      tempData.duration === duration
    ) {
      setHighestTemp(tempData.aggregates?.max);
      setTemp(tempData.data?.[tempData.data.length - 1].value);
    }
  }, [duration, pendingTempData, tempData]);

  const noPoolsLive = useMemo(() => {
    return (
      !poolsInfoStatus.pending &&
      !poolsInfoStatus.error &&
      // TODO: remove alive when cgminer is removed
      !poolsInfo?.find((pool) => /alive|active/i.test(pool?.status ?? ""))
    );
  }, [poolsInfo, poolsInfoStatus]);

  return (
    <>
      {noPoolsLive && (
        <NoPoolsCallout arePoolsConfigured={!!poolsInfo?.[0]?.url} />
      )}
      <div className="flex flex-col space-y-6">
        <div className="flex items-center">
          <div className="grow text-heading-300">Home</div>
          <DurationSelector
            className="h-fit"
            duration={duration}
            onSelect={setDuration}
          />
        </div>

        <div className="flex w-full space-x-6 phone:flex-col phone:space-y-6 phone:space-x-0">
          <EfficiencyWidget
            avgEfficiency={avgEfficiency}
            efficiencyValues={historicalEfficiency}
            duration={duration}
            loading={pendingEfficiency && !historicalEfficiency}
          />
          <PowerUsageWidget
            powerAggregates={powerAggregates}
            powerValues={historicalPower}
            duration={duration}
            loading={pendingPower && !historicalPower}
          />
          <TempWidget
            temp={temp}
            highestTemp={highestTemp}
            duration={duration}
            hashboardSerials={hashboardSerials}
            loading={
              (pendingTempData && !temp) ||
              (pendingHashboardsInfo && !hashboardSerials)
            }
          />
        </div>

        <Divider />

        <Hashrate duration={duration} hashboardSerials={hashboardSerials} />
      </div>
    </>
  );
};

export default Home;
