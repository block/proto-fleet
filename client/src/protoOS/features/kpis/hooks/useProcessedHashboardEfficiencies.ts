import { useEffect, useMemo, useState } from "react";
import { convertionFns, convertValues, downsample } from "./utility";
import { useHashboardEfficiency } from "@/protoOS/api";
import { TimeSeriesData } from "@/protoOS/api/types";
import { Duration } from "@/shared/components/DurationSelector";

type HbEfficiency = {
  name: string;
  data: TimeSeriesData[];
};

type ReducedData = {
  lowestPerformer: { name: string; avgHashrate: number } | null;
  efficiencies: HbEfficiency[];
};

type UseProcessedHashboardEfficiencyProps = {
  serials: string[];
  duration: Duration;
};

const useProcessedHashboardEfficiency = ({
  serials,
  duration,
}: UseProcessedHashboardEfficiencyProps) => {
  const [efficiencies, setEffiencies] = useState<HbEfficiency[]>([]);
  const [lowestPerformer, setLowestPerformer] = useState<string>();

  // Fetch individual hashrate data for each hashboard
  const { data: hbEfficiencyData, pending: pending } = useHashboardEfficiency({
    duration,
    hashboardSerial: serials,
    poll: true,
  });

  // Aggregate and convert hashboard hashrate data to be used in the chart.
  useEffect(() => {
    if (pending || !hbEfficiencyData) return;

    const durationsMatch = Object.values(hbEfficiencyData).every(
      (hb) => hb.duration === duration,
    );
    if (!durationsMatch) return;

    const reducedData = Object.entries(hbEfficiencyData).reduce(
      (acc, [key, value], idx) => {
        void key;
        const name = "Hashboard " + (idx + 1);

        if (acc.lowestPerformer === null) {
          acc.lowestPerformer = {
            name,
            avgHashrate: value.aggregates?.avg,
          };
        } else if (value.aggregates?.avg < acc.lowestPerformer.avgHashrate) {
          acc.lowestPerformer = {
            name,
            avgHashrate: value.aggregates?.avg,
          };
        }

        acc.efficiencies.push({
          name,
          data: convertValues(
            downsample(value.data, duration),
            convertionFns.efficiency,
          ),
        });
        return acc;
      },
      {
        lowestPerformer: null,
        efficiencies: [] as HbEfficiency[],
      } as ReducedData,
    );

    setLowestPerformer(reducedData.lowestPerformer?.name);
    setEffiencies(reducedData.efficiencies);
  }, [duration, hbEfficiencyData, pending]);

  return useMemo(() => {
    return {
      lowestPerformer,
      efficiencies,
    };
  }, [lowestPerformer, efficiencies]);
};

export default useProcessedHashboardEfficiency;
