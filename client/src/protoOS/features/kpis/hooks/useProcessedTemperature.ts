import { useEffect, useMemo, useState } from "react";
import { conversionFns } from "./utility";
import { convertAggregateValues, convertValues, downsample } from "./utility";
import { useTemperature } from "@/protoOS/api";
import { Aggregates, TimeSeriesData } from "@/protoOS/api/types";
import { type Duration } from "@/shared/components/DurationSelector";

type UseProcessedTemperatureProps = {
  duration: Duration;
};

// Aggretate and convert hashrate data to be used in the chart.
// This aggregation is different than the aggregates returned in
// the response itself, instead we are aggregating all of the
// time series data into smaller time buckets to smooth out the chart
const useProcessedTemperature = ({
  duration,
}: UseProcessedTemperatureProps) => {
  const [temperature, setTemperature] = useState<TimeSeriesData[]>([]);
  const [aggregates, setAggregates] = useState<Aggregates>({});

  // Fetch raw hashrate data from api
  const { data: rawTemperature, pending } = useTemperature({
    duration,
    poll: true,
  });

  // dump data when user changes duration
  useEffect(() => {
    setTemperature([]);
  }, [duration]);

  // downsample raw Temperature into timebuckets and
  // convert watts to kw.
  useEffect(() => {
    if (
      pending ||
      !rawTemperature?.data?.length ||
      rawTemperature.duration !== duration
    ) {
      return;
    }

    const convertedAggregates = convertAggregateValues(
      rawTemperature.aggregates,
      conversionFns.temperature,
    );

    const downsampledTemperature = downsample(rawTemperature.data, duration);
    const convertedTemperature = convertValues(
      downsampledTemperature,
      conversionFns.temperature,
    );

    setAggregates(convertedAggregates);
    setTemperature(convertedTemperature);
  }, [duration, rawTemperature, pending]);

  const processed = useMemo(() => {
    return {
      aggregates,
      temperature,
    };
  }, [temperature, aggregates]);

  return processed;
};

export default useProcessedTemperature;
