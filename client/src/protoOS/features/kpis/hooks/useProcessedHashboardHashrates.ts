import { useEffect, useMemo, useState } from "react";
import { conversionFns } from "./utility";
import { useHashboardHashrate } from "@/protoOS/api";
import { TimeSeriesData } from "@/protoOS/api/types";
import useHashboardLocationStore from "@/protoOS/store/useHashboardLocationStore";
import { convertValues, downsample } from "@/shared/components/Chart/utility";
import { Duration } from "@/shared/components/DurationSelector";

type HbHashRate = {
  name: string;
  serial: string;
  data: TimeSeriesData[];
};

type ReducedData = {
  lowestPerformer: { name: string; avgHashrate: number } | null;
  hashrates: HbHashRate[];
};

type UseProcessedHashboardHashratesProps = {
  serials: string[];
  duration: Duration;
};

const useProcessedHashboardHashrates = ({
  serials,
  duration,
}: UseProcessedHashboardHashratesProps) => {
  const [hashrates, setHashrates] = useState<HbHashRate[]>([]);
  const [lowestPerformer, setLowestPerformer] = useState<string>();
  const getSlotByHbSn = useHashboardLocationStore(
    (state) => state.getSlotByHbSn,
  );

  // Fetch individual hashrate data for each hashboard
  const { data: hbHashrateData, pending: pending } = useHashboardHashrate({
    duration,
    hashboardSerial: serials,
    poll: true,
  });

  // Aggregate and convert hashboard hashrate data to be used in the chart.
  useEffect(() => {
    if (pending || !hbHashrateData) return;

    const durationsMatch = Object.values(hbHashrateData).every(
      (hb) => hb.duration === duration,
    );
    if (!durationsMatch) return;

    const entries = Object.entries(hbHashrateData);
    const reducedData = entries
      .sort(
        (a, b) =>
          (getSlotByHbSn(a[0]) ?? entries.length) -
          (getSlotByHbSn(b[0]) ?? entries.length),
      )
      .reduce(
        (acc, [key, value]) => {
          const name = "Hashboard " + getSlotByHbSn(key);

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

          acc.hashrates.push({
            name,
            serial: key,
            data: convertValues(
              downsample(value.data, duration),
              conversionFns.hashrate,
            ),
          });
          return acc;
        },
        {
          lowestPerformer: null,
          hashrates: [] as HbHashRate[],
        } as ReducedData,
      );

    setLowestPerformer(reducedData.lowestPerformer?.name);
    setHashrates(reducedData.hashrates);
  }, [duration, hbHashrateData, pending, getSlotByHbSn]);

  return useMemo(() => {
    return {
      lowestPerformer,
      hashrates,
    };
  }, [lowestPerformer, hashrates]);
};

export default useProcessedHashboardHashrates;
