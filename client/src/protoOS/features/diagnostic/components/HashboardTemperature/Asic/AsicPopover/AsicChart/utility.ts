// TODO: [STORE_REFACTOR] Not sure we need any of these utilis anymore since moving telemetry to zustand
// This feature is currently turned off, so revisit later when we add it back in
import { ChartData } from "@/shared/components/LineChart/types";
import { getDayFromEpoch, getTimeFromEpoch } from "@/shared/utils/datetime";

interface ValueProps {
  datetime: number;
  data: ChartData[];
}

const getValue = ({ datetime, data }: ValueProps) => {
  // ignore seconds, only match up to minute
  const matchedTime = data.find(
    (d) =>
      getDayFromEpoch(d.datetime) === getDayFromEpoch(datetime) &&
      getTimeFromEpoch(d.datetime).slice(0, -3) === getTimeFromEpoch(datetime).slice(0, -3),
  );
  return matchedTime?.value ?? undefined;
};

interface ChartDataProps {
  hashrateData: ChartData[];
  temperatureData: ChartData[];
}

export const getChartData = ({ hashrateData, temperatureData }: ChartDataProps) => {
  // Handle case where we have no data
  if (!hashrateData.length && !temperatureData.length) {
    return [];
  }

  // Handle case where we only have one type of data
  if (!hashrateData.length) {
    return temperatureData.map((data) => ({
      datetime: data.datetime,
      hashrate_ghs: undefined,
      temp_c: data.value,
    }));
  }

  if (!temperatureData.length) {
    return hashrateData.map((data) => ({
      datetime: data.datetime,
      hashrate_ghs: data.value,
      temp_c: undefined,
    }));
  }

  // Handle case where we have both datasets
  const plotHashrates = hashrateData.length >= temperatureData.length;
  const dataToMap = plotHashrates ? hashrateData : temperatureData;
  const chartData = dataToMap.map((data) => {
    const datetime = data.datetime;
    return {
      datetime: datetime,
      hashrate_ghs: plotHashrates ? data.value : getValue({ datetime, data: hashrateData }),
      temp_c: plotHashrates ? getValue({ datetime, data: temperatureData }) : data.value,
    };
  });

  return chartData || [];
};
