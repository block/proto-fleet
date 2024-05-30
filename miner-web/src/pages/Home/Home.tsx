import { useContext, useEffect, useMemo, useState } from "react";

import { ApiContext, useEfficiency, useMiningStatus } from "api";

import { getDisplayValue, getTimeFromEpoch } from "common/utils/stringUtils";

import Divider from "components/Divider";
import DurationSelector, {
  Duration,
  durations,
} from "components/DurationSelector";
import AsicTempWidget from "components/InfoWidget/AsicTempWidget";
import EfficiencyWidget, {
  mockEfficiencyData,
} from "components/InfoWidget/EfficiencyWidget";
import PowerUsageWidget from "components/InfoWidget/PowerUsageWidget";

import Hashrate from "./Hashrate";
import NoPoolsCallout from "./NoPoolsCallout";

const Home = () => {
  const [duration, setDuration] = useState<Duration>(durations[0]);
  const [efficiency, setEfficiency] = useState<string | number>();
  const [historicalEfficiency, setHistoricalEfficiency] =
    useState<{ time: string; value: string | number }[]>();
  const [avgEfficiency, setAvgEfficiency] = useState<string | number>();
  const [powerUsage, setPowerUsage] = useState<string | number>();
  const [asicTemp, setAsicTemp] = useState<string | number>();
  const { data: miningStatus, pending: pendingMiningStatus } = useMiningStatus({
    poll: true,
  });
  const { data: efficiencyData } = useEfficiency({
    duration,
    poll: true,
  });
  const { poolsInfo, poolsInfoStatus } = useContext(ApiContext);

  useEffect(() => {
    if (miningStatus) {
      if (miningStatus.power_usage_watts) {
        const powerUsageKw = miningStatus.power_usage_watts / 1000;
        setPowerUsage(getDisplayValue(powerUsageKw));
        setAsicTemp(getDisplayValue(miningStatus.average_temp_c));
      }
    }
  }, [miningStatus]);

  useEffect(() => {
    if (efficiencyData && efficiencyData.data?.length) {
      // TODO: remove else when mocks moved to swagger
      const apiData = efficiencyData.data[0].datetime
        ? efficiencyData
        : mockEfficiencyData;
      setHistoricalEfficiency(
        apiData.data?.map((data) => ({
          time: getTimeFromEpoch(data.datetime),
          value: data.value || 0,
        }))
      );
      setEfficiency(
        getDisplayValue(apiData.data?.[apiData.data?.length - 1].value)
      );
      setAvgEfficiency(getDisplayValue(apiData.aggregates?.avg));
    }
  }, [efficiencyData]);

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
            efficiency={efficiency}
            avgEfficiency={avgEfficiency}
            efficiencyValues={historicalEfficiency}
          />
          <PowerUsageWidget
            loading={pendingMiningStatus}
            powerUsage={powerUsage}
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
