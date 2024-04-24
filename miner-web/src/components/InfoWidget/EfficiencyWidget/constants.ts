import { getRandomInt } from "common/utils/utility";

import { times } from "components/Chart/constants";

export const getChartData = () => {
  const chartData = times.map((time) => {
    const value = getRandomInt(20, 30);
    return {
      value,
      time,
    };
  });

  return chartData;
};
