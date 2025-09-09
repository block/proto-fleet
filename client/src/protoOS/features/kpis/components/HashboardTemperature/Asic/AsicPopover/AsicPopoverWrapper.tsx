import { useEffect, useState } from "react";

import { ChartData } from "./AsicChart/types";
import AsicPopover from "./AsicPopover";
import { convertHashrateValues, convertTemperatureValues } from "./utility";
import { useAsicHashrate, useAsicTemperature } from "@/protoOS/api";
import { AsicStats, GetAsicTemperatureParams } from "@/protoOS/api/types";
import useHashboardAsicStore from "@/protoOS/store/useHashboardAsicStore";
import { type Duration } from "@/shared/components/DurationSelector";

interface AsicPopoverWrapperProps {
  asic: AsicStats;
  duration: Duration;
  granularity: GetAsicTemperatureParams["granularity"];
  hashboardSerial: string;
  closePopover: () => void;
  closeIgnoreSelectors?: string[];
}

const AsicPopoverWrapper = ({
  asic,
  duration,
  granularity,
  hashboardSerial,
  closePopover,
  closeIgnoreSelectors,
}: AsicPopoverWrapperProps) => {
  const [temperatureData, setTemperatureData] = useState<ChartData[]>();
  const [hashrateData, setHashrateData] = useState<ChartData[]>();
  const { pending: pendingTemp } = useAsicTemperature({
    asicId: asic?.id ?? 0,
    duration,
    granularity,
    hashboardSerial,
    poll: true,
  });
  const { pending: pendingHashrate } = useAsicHashrate({
    asicId: asic?.id ?? 0,
    duration,
    granularity,
    hashboardSerial,
    poll: true,
  });

  const asicTemperatureData = useHashboardAsicStore(
    (state) =>
      state.hashboards.get(hashboardSerial)?.asics.get(asic?.id ?? 0)
        ?.tempHistory,
  );
  const asicHashrateData = useHashboardAsicStore(
    (state) =>
      state.hashboards.get(hashboardSerial)?.asics.get(asic?.id ?? 0)
        ?.hashrateHistory,
  );

  useEffect(() => {
    if (asicTemperatureData?.length) {
      setTemperatureData(convertTemperatureValues(asicTemperatureData));
    }
  }, [asicTemperatureData]);

  useEffect(() => {
    if (asicHashrateData?.length) {
      setHashrateData(convertHashrateValues(asicHashrateData));
    }
  }, [asicHashrateData]);

  return (
    <AsicPopover
      asic={asic}
      hashrateData={hashrateData}
      pendingAsicHashrateData={pendingHashrate}
      pendingAsicTemperatureData={pendingTemp}
      temperatureData={temperatureData}
      closePopover={closePopover}
      closeIgnoreSelectors={closeIgnoreSelectors}
    />
  );
};

export default AsicPopoverWrapper;
