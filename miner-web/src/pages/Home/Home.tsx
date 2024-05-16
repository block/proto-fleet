import { useContext, useEffect, useMemo, useState } from "react";

import { ApiContext, useMiningStatus } from "api";

import Divider from "components/Divider";
import DurationSelector, { Duration, durations } from "components/DurationSelector";
import AsicTempWidget from "components/InfoWidget/AsicTempWidget";
import EfficiencyWidget from "components/InfoWidget/EfficiencyWidget";
import PowerUsageWidget from "components/InfoWidget/PowerUsageWidget";

import Hashrate from "./Hashrate";
import NoPoolsCallout from "./NoPoolsCallout";

const Home = () => {
  const [duration, setDuration] = useState<Duration>(durations[0]);
  const [efficiency, setEfficiency] = useState<string>();
  const [powerUsage, setPowerUsage] = useState<string>();
  const [asicTemp, setAsicTemp] = useState<string>();
  // TODO: figure out how frequently we should be re-fetching this data
  const { data: miningStatus, pending: pendingMiningStatus } =
    useMiningStatus();
  const { poolsInfo, poolsInfoStatus } = useContext(ApiContext);

  useEffect(() => {
    if (miningStatus) {
      if (miningStatus.power_usage_watts) {
        const powerUsageKw = miningStatus.power_usage_watts / 1000;
        const powerUsageRounded = powerUsageKw.toFixed(2);
        setEfficiency(miningStatus.average_efficiency_jth?.toFixed(2));
        setPowerUsage(powerUsageRounded);
        setAsicTemp(miningStatus.average_temp_c?.toFixed(2));
      }
    }
  }, [miningStatus]);

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
          {/* TODO: send efficiency value & loading once API is implemented */}
          <EfficiencyWidget
            efficiency={efficiency}
            efficiencyValues={[
              { value: 25 },
              { value: 24 },
              { value: 29 },
              { value: 26 },
              { value: 28 },
            ]}
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
