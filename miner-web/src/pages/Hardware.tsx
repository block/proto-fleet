import { useEffect, useState } from "react";

import { useMiningStatus } from "api";

import DurationSelector from "components/DurationSelector";
import AsicTempWidget from "components/InfoWidget/AsicTempWidget";

const Hardware = () => {
  const [asicTemp, setAsicTemp] = useState<string>();
  // TODO: figure out how frequently we should be re-fetching this data
  const { data: miningStatus, pending: pendingMiningStatus } =
    useMiningStatus();

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
        {/* TODO: remove this wrapper when second widget is added as width will automatically become half */}
        <div className="flex w-1/2 phone:w-full">
          <AsicTempWidget asicTemp={asicTemp} loading={pendingMiningStatus} />
        </div>
      </div>
    </div>
  );
};

export default Hardware;
