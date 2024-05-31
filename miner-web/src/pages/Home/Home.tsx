import { useContext, useEffect, useMemo, useState } from "react";

import { ApiContext, useEfficiency, useMiningStatus, usePower } from "api";
import { Aggregates } from "apiTypes";

import { getDisplayValue } from "common/utils/stringUtils";

import Divider from "components/Divider";
import DurationSelector, {
  Duration,
  durations,
} from "components/DurationSelector";
import AsicTempWidget from "components/InfoWidget/AsicTempWidget";
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
  const [asicTemp, setAsicTemp] = useState<string | number>();
  const { data: miningStatus, pending: pendingMiningStatus } = useMiningStatus({
    poll: true,
  });
  const { data: efficiencyData, pending: pendingEfficiency } = useEfficiency({
    duration,
    poll: true,
  });
  const { data: powerData, pending: pendingPower } = usePower({
    duration,
    poll: true,
  });
  const { poolsInfo, poolsInfoStatus } = useContext(ApiContext);

  useEffect(() => {
    if (miningStatus?.average_temp_c) {
      setAsicTemp(getDisplayValue(miningStatus.average_temp_c));
    }
  }, [miningStatus]);

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

  const noPoolsLive = useMemo(() => {
    return (
      !poolsInfoStatus.pending &&
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
          <AsicTempWidget asicTemp={asicTemp} loading={pendingMiningStatus} />
        </div>

        <Divider />

        <Hashrate duration={duration} />
      </div>
    </>
  );
};

export default Home;
