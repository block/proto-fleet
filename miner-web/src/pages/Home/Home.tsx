import { useEffect, useMemo, useState } from "react";

import {
  useEfficiency,
  useHashboards,
  usePower,
  useTemperature,
} from "api";
import { Aggregates } from "apiTypes";

import { useApiContext } from "common/hooks/useApiContext";
import { useLocalStorage } from "common/hooks/useLocalStorage";
import { getDisplayValue } from "common/utils/stringUtils";

import Divider from "components/Divider";
import DurationSelector, {
  Duration,
  durations,
} from "components/DurationSelector";
import EfficiencyWidget, {
  aggregateEfficiencyValues,
  convertEfficiencyValues,
  EfficiencyValues,
} from "components/InfoWidget/EfficiencyWidget";
import PowerUsageWidget, {
  aggregatePowerValues,
  convertAggregatePowerValues,
  convertPowerValues,
} from "components/InfoWidget/PowerUsageWidget";
import TempWidget from "components/InfoWidget/TempWidget";

import Hashrate from "./Hashrate";
import NoPoolsCallout from "./NoPoolsCallout";

const Home = () => {
  const { getItem, setItem } = useLocalStorage();
  const [duration, setDuration] = useState<Duration>(
    getItem("duration") || durations[0]
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
  const { poolsInfo, poolsInfoStatus } = useApiContext();

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
          .filter(Boolean) as string[]
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
        duration
      );
      setHistoricalEfficiency(
        convertEfficiencyValues(aggregatedEfficiencyValues)
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
        duration
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
      !poolsInfo?.find((pool) => /alive|active/i.test(pool?.status || ""))
    );
  }, [poolsInfo, poolsInfoStatus]);

  return (
    <>
      {noPoolsLive && (
        <NoPoolsCallout arePoolsConfigured={!!poolsInfo?.[0]?.url} />
      )}
      <div className="flex flex-col space-y-6 h-full">
        <div className="flex items-center">
          <div className="text-heading-300 grow">Home</div>
          <DurationSelector
            className="h-fit"
            duration={duration}
            onSelect={setDuration}
          />
        </div>

        <div className="flex space-x-6 w-full phone:flex-col phone:space-x-0 phone:space-y-6">
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
