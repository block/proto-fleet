import { useContext, useEffect, useMemo, useState } from "react";

import { ApiContext, useMiningStatus } from "api";

import Divider from "components/Divider";
import DurationSelector from "components/DurationSelector";
import InfoWidget from "components/InfoWidget";
import NoPoolsCallout from "./NoPoolsCallout";
import AsicTempWidget from "./Widgets/AsicTempWidget";
import EfficiencyWidget from "./Widgets/EfficiencyWidget";
import PowerUsageWidget from "./Widgets/PowerUsageWidget";

const Home = () => {
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
        setPowerUsage(powerUsageRounded);
        setAsicTemp(miningStatus.temp_c?.toFixed(2));
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
      {noPoolsLive && <NoPoolsCallout arePoolsConfigured={!!poolsInfo[0]?.url} />}
      <div className="flex flex-col space-y-6">
        <div className="flex items-center">
          <div className="text-heading-300 grow">Home</div>
          <DurationSelector className="h-fit" />
        </div>

        <div className="flex space-x-6 w-full">
          {/* TODO: send efficiency value & loading once API is implemented */}
          <EfficiencyWidget />
          <PowerUsageWidget
            loading={pendingMiningStatus}
            powerUsage={powerUsage}
          />
          <AsicTempWidget asicTemp={asicTemp} loading={pendingMiningStatus} />
        </div>

        <Divider />

        <div className="flex space-x-6 w-full">
          {/* TODO: display hashrate values once API is implemented */}
          <InfoWidget
            title="Current Hashrate"
            value={undefined}
            loading={false}
          />
          <InfoWidget title="Average" value={undefined} loading={false} />
          <InfoWidget title="Lowest" value={undefined} loading={false} />
          <InfoWidget title="Highest" value={undefined} loading={false} />
        </div>
      </div>
    </>
  );
};

export default Home;
