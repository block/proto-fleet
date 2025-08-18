import { useEffect, useState } from "react";
import { conversionFns } from "./utility";
import { convertValues, downsample } from "./utility";
import { useHashboardPower } from "@/protoOS/api";
import { TimeSeriesData } from "@/protoOS/api/types";
import useHashboardLocationStore from "@/protoOS/store/useHashboardLocationStore";
import { Duration } from "@/shared/components/DurationSelector";

type HbPower = {
  name: string;
  serial: string;
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
  const getSlotByHbSn = useHashboardLocationStore(
    (state) => state.getSlotByHbSn,
  );

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

    const entries = Object.entries(hbPowerData);
    const downsampledHbPowerUsage = entries
      .sort(
        (a, b) =>
          (getSlotByHbSn(a[0]) ?? entries.length) -
          (getSlotByHbSn(b[0]) ?? entries.length),
      )
      .reduce((acc, [key, value]) => {
        const slot = getSlotByHbSn(key);
        const name = "Hashboard " + slot;
        acc.push({
          name,
          serial: key,
          data: convertValues(
            downsample(value.data, duration),
            conversionFns.powerUsage,
          ),
        });
        return acc;
      }, [] as HbPower[]);

    setPowerUsages(downsampledHbPowerUsage);
  }, [duration, hbPowerData, pending, getSlotByHbSn]);

  return powerUsages;
};

export default useProcessedHashboardPowerUsages;
