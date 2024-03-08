import { useEffect, useState } from "react";

import { useMiningStatus } from "api";

import Divider from "components/Divider";
import DurationSelector from "components/DurationSelector";
import InfoWidget from "components/InfoWidget";
import AsicTempWidget from "./Widgets/AsicTempWidget";
import EfficiencyWidget from "./Widgets/EfficiencyWidget";
import PowerUsageWidget from "./Widgets/PowerUsageWidget";

const Home = () => {
  const [powerUsage, setPowerUsage] = useState<string>();
  const [asicTemp, setAsicTemp] = useState<string>();
  // TODO: figure out how frequently we should be re-fetching this data
  const { data: miningStatus, pending: pendingMiningStatus } =
    useMiningStatus();

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

  return (
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
  );
};

export default Home;
