import { useEffect, useState } from "react";

// import { dangerTemp } from "../../constants";
import { ChartData } from "./AsicChart/types";
import AsicPopover from "./AsicPopover";
import { convertHashrateValues, convertTemperatureValues } from "./utility";
import { useAsicHashrate, useAsicTemperature } from "@/protoOS/api";
import { AsicStats, GetAsicTemperatureParams } from "@/protoOS/api/types";

interface AsicPopoverWrapperProps {
  asic: AsicStats;
  duration: GetAsicTemperatureParams["duration"];
  granularity: GetAsicTemperatureParams["granularity"];
  hashboardSerial: string;
}

const AsicPopoverWrapper = ({
  asic,
  duration,
  granularity,
  hashboardSerial,
}: AsicPopoverWrapperProps) => {
  const [temperatureData, setTemperatureData] = useState<ChartData[]>();
  const [hashrateData, setHashrateData] = useState<ChartData[]>();
  const { data: asicTemperatureData, pending: pendingAsicTemperatureData } =
    useAsicTemperature({
      asicId: asic.id,
      duration,
      granularity,
      hashboardSerial,
      poll: true,
    });
  const { data: asicHashrateData, pending: pendingAsicHashrateData } =
    useAsicHashrate({
      asicId: asic.id,
      duration,
      granularity,
      hashboardSerial,
      poll: true,
    });

  useEffect(() => {
    if (asicTemperatureData?.data?.length) {
      setTemperatureData(convertTemperatureValues(asicTemperatureData.data));
    }
  }, [asicTemperatureData]);

  useEffect(() => {
    if (asicHashrateData?.data?.length) {
      setHashrateData(convertHashrateValues(asicHashrateData.data));
    }
  }, [asicHashrateData]);

  return (
    <AsicPopover
      asic={asic}
      hashrateData={hashrateData}
      pendingAsicHashrateData={pendingAsicHashrateData}
      pendingAsicTemperatureData={pendingAsicTemperatureData}
      temperatureData={temperatureData}
    />
  );
};

export default AsicPopoverWrapper;
