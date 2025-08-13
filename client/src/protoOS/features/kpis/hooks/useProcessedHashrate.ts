import { useEffect, useMemo, useState } from "react";
import { conversionFns } from "./utility";
import { useHashrate } from "@/protoOS/api";
import { Aggregates, TimeSeriesData } from "@/protoOS/api/types";
import {
  convertAggregateValues,
  convertValues,
  downsample,
} from "@/shared/components/Chart/utility";
import { type Duration } from "@/shared/components/DurationSelector";

type UseProcessedHashrateProps = {
  duration: Duration;
};

// Aggretate and convert hashrate data to be used in the chart.
// This aggregation is different than the aggregates returned in
// the response itself, instead we are aggregating all of the
// time series data into smaller time buckets to smooth out the chart
const useProcessedHashrate = ({ duration }: UseProcessedHashrateProps) => {
  const [hashrate, setHashrate] = useState<TimeSeriesData[]>([]);
  const [aggregates, setAggregates] = useState<Aggregates>({});

  // Fetch raw hashrate data from api
  const { data: rawHashrate, pending } = useHashrate({
    duration,
    poll: true,
  });

  // dump data when user changes duration
  useEffect(() => {
    setHashrate([]);
  }, [duration]);

  // downsample raw hashrate into timebuckets and
  // convert hashrate values to be used in the chart.
  useEffect(() => {
    if (
      pending ||
      !rawHashrate?.data?.length ||
      rawHashrate.duration !== duration
    ) {
      return;
    }

    const convertedAggregates = convertAggregateValues(
      rawHashrate.aggregates,
      conversionFns.hashrate,
    );

    const downsampledHashrateValues = downsample(rawHashrate.data, duration);
    const convertedHashrate = convertValues(
      downsampledHashrateValues,
      conversionFns.hashrate,
    );

    setAggregates(convertedAggregates || {});
    setHashrate(convertedHashrate);
  }, [duration, rawHashrate, pending]);

  const processed = useMemo(() => {
    return {
      hashrate,
      aggregates,
    };
  }, [hashrate, aggregates]);

  return processed;
};

export default useProcessedHashrate;
