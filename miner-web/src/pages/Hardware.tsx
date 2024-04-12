import { useEffect, useState } from "react";

import { useCoolingStatus, useMiningStatus } from "api";

import DurationSelector from "components/DurationSelector";
import AsicTempWidget from "components/InfoWidget/AsicTempWidget";
import FanSpeedWidget from "components/InfoWidget/FanSpeedWidget";

const Hardware = () => {
  const [asicTemp, setAsicTemp] = useState<string>();
  // TODO: figure out how frequently we should be re-fetching this data
  const { data: miningStatus, pending: pendingMiningStatus } =
    useMiningStatus();
  const { data: coolingStatus, pending: pendingCoolingStatus } =
    useCoolingStatus();

  useEffect(() => {
    if (miningStatus) {
      setAsicTemp(miningStatus.temp_c?.toFixed(2));
    }
  }, [miningStatus]);

  return (
    <div className="flex flex-col space-y-6">
      <div className="flex items-center">
        <div className="text-heading-300 grow">Hardware</div>
        <DurationSelector className="h-fit" />
      </div>

      <div className="flex space-x-6 w-full phone:flex-col phone:space-x-0 phone:space-y-6">
        <AsicTempWidget asicTemp={asicTemp} loading={pendingMiningStatus} />
        <FanSpeedWidget
          fanSpeeds={
            coolingStatus?.fans
              ? [
                  ...coolingStatus.fans,
                  // Remove these when we have real fan data
                  { rpm: 3049 },
                  { rpm: 6800 },
                  { rpm: 6730 },
                ]
              : undefined
          }
          loading={pendingCoolingStatus}
        />
      </div>
    </div>
  );
};

export default Hardware;
