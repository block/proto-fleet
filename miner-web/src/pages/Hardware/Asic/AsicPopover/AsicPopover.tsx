import { useEffect, useState } from "react";

import { useAsicHashrate, useAsicTemperature } from "api";
import { AsicStats, HashrateResponseHashratedata } from "apiTypes";

import { positions } from "common/constants";
import { getDisplayValue } from "common/utils/stringUtils";

import Popover from "components/Popover";

// import { dangerTemp } from "../../constants";
import { getRowLabel } from "../../utility";
import AsicChart from "./AsicChart";
import { ChartData } from "./AsicChart/types";
import AsicPopoverRow from "./AsicPopoverRow";
import { mockAsicHashrateData, mockAsicTemperatureData } from "./constants";
import { convertHashrateValues, convertTemperatureValues } from "./utility";

interface AsicPopoverProps {
  asic: AsicStats;
  duration: HashrateResponseHashratedata["duration"];
  hashboardSerial: string;
}

const AsicPopover = ({ asic, duration, hashboardSerial }: AsicPopoverProps) => {
  const [temperatureData, setTemperatureData] = useState<ChartData[]>();
  const [hashrateData, setHashrateData] = useState<ChartData[]>();
  const { data: asicTemperatureData } = useAsicTemperature({
    asicID: asic.id,
    duration,
    hashboardSerial,
    poll: true,
  });
  const { data: asicHashrateData } = useAsicHashrate({
    asicID: asic.id,
    duration,
    hashboardSerial,
    poll: true,
  });

  useEffect(() => {
    if (asicTemperatureData?.data && asicTemperatureData.data.length) {
      // TODO: remove else when mocks moved to swagger
      const apiData = asicTemperatureData.data[0].datetime
        ? asicTemperatureData
        : mockAsicTemperatureData;
      setTemperatureData(convertTemperatureValues(apiData.data));
    }
  }, [asicTemperatureData]);

  useEffect(() => {
    if (asicHashrateData?.data && asicHashrateData.data.length) {
      // TODO: remove else when mocks moved to swagger
      const apiData = asicHashrateData.data[0].datetime
        ? asicHashrateData
        : mockAsicHashrateData;
      setHashrateData(convertHashrateValues(apiData.data));
    }
  }, [asicHashrateData]);

  return (
    <Popover
      position={positions["top right"]}
      className="mb-[58px] -left-[115px] pb-3 phone:left-0 phone:top-0 phone:mb-0 h-fit"
    >
      <div className="space-y-1">
        <div className="text-200 text-text-primary/70">ASIC</div>
        <div className="text-heading-200 text-text-primary/90">
          {getRowLabel(asic.row || 0)}
          {(asic.column || 0) + 1}
        </div>
        {/* TODO: update this condition when we have set indicators */}
        {/* {(asic.temp_c || 0) >= dangerTemp && (
          <div className="text-200 text-intent-warning-text text-wrap">
            Based on historical behavior, it’s likely this ASIC will cause the
            board to overheat.
          </div>
        )} */}
      </div>
      <div className="w-[272px] h-[92px]">
        {hashrateData && temperatureData && (
          <AsicChart
            hashrateData={hashrateData}
            temperatureData={temperatureData}
          />
        )}
      </div>
      <div>
        <AsicPopoverRow
          label="Temperature"
          value={
            temperatureData?.length &&
            `${getDisplayValue(temperatureData[temperatureData.length - 1].value)}º`
          }
          className="text-core-accent-fill"
        />
        <AsicPopoverRow
          label="Hashrate"
          value={
            hashrateData?.length &&
            `${getDisplayValue(hashrateData[hashrateData.length - 1].value)} TH/s`
          }
          className="text-text-primary"
        />
      </div>
    </Popover>
  );
};

export default AsicPopover;
