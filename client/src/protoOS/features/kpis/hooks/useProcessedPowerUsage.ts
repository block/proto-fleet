import { useEffect, useMemo, useState } from "react";
import {
  convertAggregateValues,
  convertionFns,
  convertValues,
  downsample,
} from "./utility";
import { usePower } from "@/protoOS/api";
import { Aggregates, TimeSeriesData } from "@/protoOS/api/types";
import { type Duration } from "@/shared/components/DurationSelector";

type UseProcessedPowerUsageProps = {
  duration: Duration;
};

// Aggretate and convert hashrate data to be used in the chart.
// This aggregation is different than the aggregates returned in
// the response itself, instead we are aggregating all of the
// time series data into smaller time buckets to smooth out the chart
const useProcessedPowerUsage = ({ duration }: UseProcessedPowerUsageProps) => {
  const [powerUsage, setPowerUsage] = useState<TimeSeriesData[]>([]);
  const [aggregates, setAggregates] = useState<Aggregates>({});

  // Fetch raw hashrate data from api
  const { data: rawPowerUsage, pending } = usePower({
    duration,
    poll: true,
  });

  // dump data when user changes duration
  useEffect(() => {
    setPowerUsage([]);
  }, [duration]);

  // downsample raw powerUsage into timebuckets and
  // convert watts to kw.
  useEffect(() => {
    if (
      pending ||
      !rawPowerUsage?.data?.length ||
      rawPowerUsage.duration !== duration
    ) {
      return;
    }

    const convertedAggregates = convertAggregateValues(
      rawPowerUsage.aggregates,
      convertionFns.powerUsage,
    );

    const downsampledPowerUsage = downsample(rawPowerUsage.data, duration);
    const convertedPowerUsage = convertValues(
      downsampledPowerUsage,
      convertionFns.powerUsage,
    );

    setAggregates(convertedAggregates);
    setPowerUsage(convertedPowerUsage);
  }, [duration, rawPowerUsage, pending]);

  const processed = useMemo(() => {
    return {
      aggregates,
      powerUsage,
    };
  }, [powerUsage, aggregates]);

  return processed;
};

export default useProcessedPowerUsage;
