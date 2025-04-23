import { TimeSeries } from "./types";
import { TimeSeriesData } from "@/protoOS/api/types";
import { getDayFromEpoch, getTimeFromEpoch } from "@/shared/utils/datetime";

export const getPoint = (index: number, firstPoint: number, gap: number) => {
  return firstPoint + index * gap;
};

type GetChartValueArgs = {
  datetime: TimeSeriesData["datetime"];
  values: TimeSeriesData[];
};

/**
 * Returns the value of a series at a specific datetime
 * used to combine the values of multiple series into one object
 * to be used in the chart
 * @param datetime - Datetime to get value for
 * @param values - Array of TimeSeriesPoint data points
 * @returns
 */
const getChartValue = ({ datetime, values }: GetChartValueArgs) => {
  // ignore seconds, only match up to minute
  // TODO: this may become performance bottleneck if there are many data points
  // if we can assume that data is already sorted by datetime we could do binary search
  const matchedTime = values.find(
    (value) =>
      getDayFromEpoch(value.datetime) === getDayFromEpoch(datetime) &&
      getTimeFromEpoch(value.datetime).slice(0, -3) ===
        getTimeFromEpoch(datetime).slice(0, -3),
  );
  return matchedTime?.value || 0;
};

type GetChartDataArgs = {
  series: TimeSeries[];
  aggregateSeries: TimeSeries;
  units?: string;
};

export type ChartData = {
  datetime?: TimeSeriesData["datetime"];
  aggregateName: string;
  units?: string;
} & {
  [key: string]: number | string | undefined;
};

/**
 * Converts inidividual series data points into one object with all series data points at each timestamp
 * @param series - Array of TimeSeries data points
 * @param aggregateSeries - Precalculated aggregate of all series
 * @returns
 */
export const getChartData = ({
  series,
  aggregateSeries,
  units,
}: GetChartDataArgs) => {
  const chartData = aggregateSeries.data.map((totalPoint) => {
    return series.reduce(
      (acc, curr) => {
        if (curr.data.length) {
          acc[curr.name] = getChartValue({
            datetime: totalPoint.datetime,
            values: curr.data,
          });
        }

        return acc;
      },
      {
        datetime: totalPoint.datetime,
        aggregateName: aggregateSeries.name,
        units,
        [aggregateSeries.name]: totalPoint.value,
      } as ChartData,
    );
  });

  return chartData || [];
};
