import { defaultHashboardColor, hashboardColors } from "./constants";
import { TimeSeries, TimeSeriesWithSerial } from "./types";
import { TimeSeriesDataPoint } from "@/shared/features/kpis";
import { getDayFromEpoch, getTimeFromEpoch } from "@/shared/utils/datetime";

export const getPoint = (index: number, firstPoint: number, gap: number) => {
  return firstPoint + index * gap;
};

type GetChartValueArgs = {
  datetime: number | undefined;
  values: TimeSeriesDataPoint[];
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
  if (!datetime) return 0;

  // ignore seconds, only match up to minute
  // TODO: this may become performance bottleneck if there are many data points
  // if we can assume that data is already sorted by datetime we could do binary search
  const matchedTime = values.find((value) => {
    if (!value.datetime) return false;
    return (
      getDayFromEpoch(value.datetime) === getDayFromEpoch(datetime) &&
      getTimeFromEpoch(value.datetime).slice(0, -3) ===
        getTimeFromEpoch(datetime).slice(0, -3)
    );
  });
  return matchedTime?.value || 0;
};

type GetChartDataArgs = {
  series: TimeSeriesWithSerial[];
  aggregateSeries: TimeSeries;
  units?: string;
};

export type ChartData = {
  datetime?: number;
  aggregateName: string;
  units?: string;
} & {
  [key: string]: number | string | undefined;
};

/**
 * Converts individual series data points into one object with all series data points at each timestamp
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
          acc[curr.serial] = getChartValue({
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

export const getHashboardColor = (slot: number | null) => {
  if (slot === null) return defaultHashboardColor;
  return hashboardColors[(slot - 1) % hashboardColors.length];
};
