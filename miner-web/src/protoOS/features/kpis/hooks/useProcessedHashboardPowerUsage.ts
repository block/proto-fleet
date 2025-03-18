import { useEffect, useState } from "react";
import { convertionFns, convertValues, downsample } from "./utility";
import { useHashboardPower } from "@/protoOS/api";
import { TimeSeriesData } from "@/protoOS/api/types";
import { Duration } from "@/shared/components/DurationSelector";

type HbPower = {
  name: string;
  data: TimeSeriesData[];
};

type UseProcessedHashboardPowerUsagesProps = {
  serials: string[];
  duration: Duration;
};

const useProcessedHashboardPowerUsages = ({
  serials,
  duration,
}: UseProcessedHashboardPowerUsagesProps) => {
  const [powerUsages, setPowerUsages] = useState<HbPower[]>([]);

  // Fetch individual Power data for each hashboard
  const { data: hbPowerData, pending: pending } = useHashboardPower({
    duration,
    hashboardSerial: serials,
    poll: true,
  });

  // Aggregate and convert hashboard Power data to be used in the chart.
  useEffect(() => {
    if (pending || !hbPowerData) return;

    const durationsMatch = Object.values(hbPowerData).every(
      (hb) => hb.duration === duration,
    );
    if (!durationsMatch) return;

    const downsampledHbPowerUsage = Object.entries(hbPowerData).reduce(
      (acc, [key, value], idx) => {
        void key;
        const name = "Hashboard " + (idx + 1);
        acc.push({
          name,
          data: convertValues(
            downsample(value.data, duration),
            convertionFns.powerUsage,
          ),
        });
        return acc;
      },
      [] as HbPower[],
    );

    setPowerUsages(downsampledHbPowerUsage);
  }, [duration, hbPowerData, pending]);

  return powerUsages;
};

export default useProcessedHashboardPowerUsages;
