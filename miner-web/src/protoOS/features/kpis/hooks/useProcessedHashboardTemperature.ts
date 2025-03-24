import { useEffect, useState } from "react";
import {
  convertAggregateValues,
  convertionFns,
  convertValues,
  downsample,
} from "./utility";
import { useHashboardTemperature } from "@/protoOS/api";
import { Aggregates, TimeSeriesData } from "@/protoOS/api/types";
import { Duration } from "@/shared/components/DurationSelector";

export type HbTemperature = {
  name: string;
  serial: string;
  data: TimeSeriesData[];
  aggregates: Aggregates;
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

    const downsampledHBTemperatures = Object.entries(hbTemperatures).reduce(
      (acc, [key, value], idx) => {
        void key;
        const name = "Hashboard " + (idx + 1);
        acc.push({
          name,
          serial: key,
          data: convertValues(
            downsample(value.data, duration),
            convertionFns.temperature,
          ),
          aggregates: convertAggregateValues(
            value.aggregates,
            convertionFns.temperature,
          ),
        });
        return acc;
      },
      [] as HbTemperature[],
    );

    setTemperatures(downsampledHBTemperatures);
  }, [duration, hbTemperatures, pending]);

  return temperatures;
};

export default useProcessedHashboardTemperature;
