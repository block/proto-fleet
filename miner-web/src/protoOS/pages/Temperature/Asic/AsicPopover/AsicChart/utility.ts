import { ChartData } from "./types";
import { getDayFromEpoch, getTimeFromEpoch } from "@/shared/utils/stringUtils";

interface ValueProps {
  datetime: number;
  data: ChartData[];
}

const getValue = ({ datetime, data }: ValueProps) => {
  // ignore seconds, only match up to minute
  const matchedTime = data.find(
    (d) =>
      getDayFromEpoch(d.datetime) === getDayFromEpoch(datetime) &&
      getTimeFromEpoch(d.datetime).slice(0, -3) ===
        getTimeFromEpoch(datetime).slice(0, -3),
  );
  return matchedTime?.value ?? undefined;
};

interface ChartDataProps {
  hashrateData: ChartData[];
  temperatureData: ChartData[];
}

export const getChartData = ({
  hashrateData,
  temperatureData,
}: ChartDataProps) => {
  const plotHashrates = hashrateData.length >= temperatureData.length;
  const dataToMap = plotHashrates ? hashrateData : temperatureData;
  const chartData = dataToMap.map((data) => {
    const datetime = data.datetime;
    return {
      datetime: datetime,
      hashrate_ghs: plotHashrates
        ? data.value
        : getValue({ datetime, data: hashrateData }),
      temp_c: plotHashrates
        ? getValue({ datetime, data: temperatureData })
        : data.value,
    };
  });

  return chartData || [];
};
