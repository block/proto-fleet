import { useContext, useEffect, useMemo, useState } from "react";

import {
  ApiContext,
  useEfficiency,
  useHashboards,
  usePower,
  useTemperature,
} from "api";
import { Aggregates } from "apiTypes";

import { getDisplayValue } from "common/utils/stringUtils";

import Divider from "components/Divider";
import DurationSelector, {
  Duration,
  durations,
} from "components/DurationSelector";
import EfficiencyWidget, {
  mockEfficiencyData,
} from "components/InfoWidget/EfficiencyWidget";
import { convertEfficiencyValues } from "components/InfoWidget/EfficiencyWidget/utility";
import PowerUsageWidget, {
  mockPowerData,
} from "components/InfoWidget/PowerUsageWidget";
import {
  convertAggregatePowerValues,
  convertPowerValues,
} from "components/InfoWidget/PowerUsageWidget/utility";
import TempWidget, {
  mockTemperatureData,
} from "components/InfoWidget/TempWidget";

import Hashrate from "./Hashrate";
import NoPoolsCallout from "./NoPoolsCallout";

const Home = () => {
  const [duration, setDuration] = useState<Duration>(durations[0]);
  const [historicalEfficiency, setHistoricalEfficiency] =
    useState<{ time: string; value: string | number }[]>();
  const [avgEfficiency, setAvgEfficiency] = useState<string | number>();
  const [historicalPower, setHistoricalPower] =
    useState<{ time: string; value: string | number }[]>();
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
  const { poolsInfo, poolsInfoStatus } = useContext(ApiContext);

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
    if (efficiencyData && efficiencyData.data?.length) {
      // TODO: remove else when mocks moved to swagger
      const apiData = efficiencyData.data[0].datetime
        ? efficiencyData
        : mockEfficiencyData;
      setHistoricalEfficiency(convertEfficiencyValues(apiData.data));
      setAvgEfficiency(getDisplayValue(apiData.aggregates?.avg));
    }
  }, [efficiencyData]);

  useEffect(() => {
    if (powerData && powerData.data?.length) {
      // TODO: remove else when mocks moved to swagger
      const apiData = powerData.data[0].datetime ? powerData : mockPowerData;
      setHistoricalPower(convertPowerValues(apiData.data));
      setPowerAggregates(convertAggregatePowerValues(apiData.aggregates));
    }
  }, [powerData]);

  useEffect(() => {
    if (tempData && tempData.data?.length) {
      // TODO: remove else when mocks moved to swagger
      const apiData = tempData.data[0].datetime
        ? tempData
        : mockTemperatureData;
      setHighestTemp(apiData.aggregates?.max);
      setTemp(apiData.data?.[apiData.data.length - 1].value);
    }
  }, [tempData]);

  const noPoolsLive = useMemo(() => {
    return (
      !poolsInfoStatus.pending &&
      !poolsInfoStatus.error &&
      !poolsInfo.find((pool) => pool?.status === "Alive")
    );
  }, [poolsInfo, poolsInfoStatus]);

  return (
    <>
      {noPoolsLive && (
        <NoPoolsCallout arePoolsConfigured={!!poolsInfo[0]?.url} />
      )}
      <div className="flex flex-col space-y-6">
        <div className="flex items-center">
          <div className="text-heading-300 grow">Home</div>
          <DurationSelector className="h-fit" onSelect={setDuration} />
        </div>

        <div className="flex space-x-6 w-full phone:flex-col phone:space-x-0 phone:space-y-6">
          <EfficiencyWidget
            avgEfficiency={avgEfficiency}
            efficiencyValues={historicalEfficiency}
            loading={pendingEfficiency}
          />
          <PowerUsageWidget
            powerAggregates={powerAggregates}
            powerValues={historicalPower}
            loading={pendingPower}
          />
          <TempWidget
            temp={temp}
            highestTemp={highestTemp}
            duration={duration}
            hashboardSerials={hashboardSerials}
            loading={pendingTempData || pendingHashboardsInfo}
          />
        </div>

        <Divider />

        <Hashrate duration={duration} hashboardSerials={hashboardSerials} />
      </div>
    </>
  );
};

export default Home;
