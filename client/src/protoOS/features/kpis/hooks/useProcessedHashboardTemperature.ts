import { useEffect, useState } from "react";
import { useHashboardTemperature } from "@/protoOS/api";
import { Aggregates, TimeSeriesData } from "@/protoOS/api/types";
import useHashboardLocationStore from "@/protoOS/store/useHashboardLocationStore";
import {
  conversionFns,
  convertAggregateValues,
  convertValues,
  downsample,
} from "@/shared/components/Chart/utility";
import { Duration } from "@/shared/components/DurationSelector";

export type HbTemperature = {
  name: string;
  serial: string;
  data: TimeSeriesData[];
  aggregates: Aggregates;
  slot: number;
};

type UseProcessedHashboardTemperatureProps = {
  serials: string[];
  duration: Duration;
};

const useProcessedHashboardTemperature = ({
  serials,
  duration,
}: UseProcessedHashboardTemperatureProps) => {
  const [temperatures, setTemperatures] = useState<HbTemperature[]>([]);
  const getSlotByHbSn = useHashboardLocationStore(
    (state) => state.getSlotByHbSn,
  );

  // Fetch individual Power data for each hashboard
  const { data: hbTemperatures, pending } = useHashboardTemperature({
    duration,
    hashboardSerial: serials,
    poll: true,
  });

  // Aggregate and convert hashboard Power data to be used in the chart.
  useEffect(() => {
    if (pending || !hbTemperatures) return;

    const durationsMatch = Object.values(hbTemperatures).every(
      (hb) => hb.duration === duration,
    );
    if (!durationsMatch) return;

    const entries = Object.entries(hbTemperatures);
    const downsampledHBTemperatures = entries
      .sort(
        (a, b) =>
          (getSlotByHbSn(a[0]) ?? entries.length) -
          (getSlotByHbSn(b[0]) ?? entries.length),
      )
      .reduce((acc, [key, value]) => {
        const slot = getSlotByHbSn(key) || 0;
        const name = "Hashboard " + slot;
        acc.push({
          slot,
          name,
          serial: key,
          data: convertValues(
            downsample(value.data, duration),
            conversionFns.temperature,
          ),
          aggregates: convertAggregateValues(
            value.aggregates,
            conversionFns.temperature,
          ),
        });
        return acc;
      }, [] as HbTemperature[]);

    setTemperatures(downsampledHBTemperatures);
  }, [duration, hbTemperatures, pending, getSlotByHbSn]);

  return temperatures;
};

export default useProcessedHashboardTemperature;
