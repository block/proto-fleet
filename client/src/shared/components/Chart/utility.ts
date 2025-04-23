import { TimeSeriesData } from "@/protoOS/api/types";

import { getDateFromEpoch } from "@/shared/utils/datetime";

interface AxisTickOffsetProps {
  chartType?: "line" | "bar";
  firstTick: boolean;
  hasDate?: boolean;
  midTick: boolean;
  lastTick: boolean;
  payloadOffset: number;
  x: number;
}

const offsets = {
  line: {
    first: 25,
    firstDate: 16,
    mid: 16,
    midDate: 25,
    last: 0,
    lastDate: 42,
  },
  bar: {
    first: 17,
    firstDate: 26,
    mid: 15,
    midDate: 24,
    last: 15,
    lastDate: 24,
  },
};

export const getAxisTickOffset = ({
  chartType = "line",
  firstTick,
  hasDate,
  midTick,
  lastTick,
  payloadOffset,
  x,
}: AxisTickOffsetProps) => {
  let xOffset = 0;
  const isLineChart = chartType === "line";
  if (firstTick) {
    // the offset needed to add margin left to the first tick
    if (isLineChart) {
      const dateOffset = hasDate ? offsets.line.firstDate : 0;
      xOffset = offsets.line.first + payloadOffset + dateOffset;
    } else {
      const dateOffset = hasDate ? offsets.bar.firstDate : 0;
      xOffset = x - (offsets.bar.first + payloadOffset) + dateOffset;
    }
  } else if (midTick) {
    // the offset needed to center the mid ticks
    if (isLineChart) {
      const dateOffset = hasDate ? offsets.line.midDate : 0;
      xOffset = offsets.line.mid + payloadOffset + dateOffset;
    } else {
      const dateOffset = hasDate ? offsets.bar.midDate : 0;
      xOffset = offsets.bar.mid + dateOffset;
    }
  } else if (lastTick) {
    // the offset needed to add margin right to the first tick
    if (isLineChart) {
      const dateOffset = hasDate ? offsets.line.lastDate : 0;
      xOffset = offsets.line.last + payloadOffset + dateOffset;
    } else {
      const dateOffset = hasDate ? offsets.bar.lastDate : 0;
      xOffset = offsets.bar.last + dateOffset;
    }
  }
  return xOffset;
};

/**
 * Aggregates time series data points into time buckets and calculates the average value for each bucket.
 *
 * @param {TimeSeriesData[]} dataToAggregate - Array of time series data points to aggregate. Each point should have datetime and value properties.
 * @param {number} compareTimeMinutes - Time interval in minutes that defines the size of each time bucket.
 * @returns {TimeSeriesData[]} - Array of aggregated time series data with averaged values.
 *
 * @description
 * The function works by:
 * 1. Creating time buckets based on the specified interval (compareTimeMinutes)
 * 2. Grouping data points that fall within the same time bucket
 * 3. Calculating the average value for each bucket by summing values and dividing by count
 * 4. Returns a new array with the same datetime as the first point in each bucket and the average value
 */
export const aggregateValues = (
  dataToAggregate: TimeSeriesData[] = [],
  compareTimeMinutes: number,
) => {
  // if data is empty, we have not received any data from the server
  // so no need to aggregate data
  if (dataToAggregate.length === 0) {
    return dataToAggregate;
  }

  let aggregatedData = [
    { datetime: dataToAggregate[0].datetime, value: 0, numberOfValues: 0 },
  ];
  const currentDateEpoch = getDateFromEpoch(
    dataToAggregate[0].datetime,
  ).setSeconds(0);
  let currentDate = getDateFromEpoch(currentDateEpoch);
  dataToAggregate.forEach((data) => {
    const dateToCompareEpoch = getDateFromEpoch(data.datetime).setSeconds(0);
    const dateToCompare = getDateFromEpoch(dateToCompareEpoch);
    const diffMs = dateToCompare.getTime() - currentDate.getTime();
    const diffMins = diffMs / 60000;
    if (diffMins < compareTimeMinutes) {
      aggregatedData[aggregatedData.length - 1] = {
        datetime: aggregatedData[aggregatedData.length - 1].datetime,
        value:
          +aggregatedData[aggregatedData.length - 1].value + +(data.value || 0),
        numberOfValues:
          aggregatedData[aggregatedData.length - 1].numberOfValues + 1,
      };
    } else {
      currentDate = getDateFromEpoch(dateToCompareEpoch);
      aggregatedData.push({
        datetime: data.datetime,
        value: +(data.value || 0),
        numberOfValues: 1,
      });
    }
  });
  return aggregatedData.map((data) => ({
    datetime: data.datetime,
    value: +data.value / data.numberOfValues,
  }));
};
