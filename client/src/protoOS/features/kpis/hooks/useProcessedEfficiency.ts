import { useEffect, useMemo, useState } from "react";
import { useEfficiency } from "@/protoOS/api";
import { Aggregates, TimeSeriesData } from "@/protoOS/api/types";
import {
  conversionFns,
  convertAggregateValues,
  convertValues,
  downsample,
} from "@/shared/components/Chart/utility";
import { type Duration } from "@/shared/components/DurationSelector";

type UseProcessedEfficiencyProps = {
  duration: Duration;
};

// Aggretate and convert hashrate data to be used in the chart.
// This aggregation is different than the aggregates returned in
// the response itself, instead we are aggregating all of the
// time series data into smaller time buckets to smooth out the chart
const useProcessedEfficiency = ({ duration }: UseProcessedEfficiencyProps) => {
  const [efficiency, setEfficiency] = useState<TimeSeriesData[]>([]);
  const [aggregates, setAggregates] = useState<Aggregates>({});

  // Fetch raw hashrate data from api
  const { data: rawEfficiency, pending } = useEfficiency({
    duration,
    poll: true,
  });

  // dump data when user changes duration
  useEffect(() => {
    setEfficiency([]);
  }, [duration]);

  // downsample raw efficiency into timebuckets and
  // convert hashrate values to be used in the chart.
  useEffect(() => {
    if (
      pending ||
      !rawEfficiency?.data?.length ||
      rawEfficiency.duration !== duration
    ) {
      return;
    }

    const convertedAggregates = convertAggregateValues(
      rawEfficiency.aggregates,
      conversionFns.efficiency,
    );

    const downsampledEfficiency = downsample(rawEfficiency.data, duration);
    const convertedEfficiency = convertValues(
      downsampledEfficiency,
      conversionFns.efficiency,
    );

    setAggregates(convertedAggregates);
    setEfficiency(convertedEfficiency);
  }, [duration, rawEfficiency, pending]);

  const processed = useMemo(() => {
    return {
      efficiency,
      aggregates,
    };
  }, [efficiency, aggregates]);

  return processed;
};

export default useProcessedEfficiency;
