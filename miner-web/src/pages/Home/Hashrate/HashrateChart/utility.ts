import { getDayFromEpoch, getTimeFromEpoch } from "common/utils/stringUtils";
import { Hashrates } from "../types";

export const getPoint = (index: number, firstPoint: number, gap: number) => {
  return firstPoint + index * gap;
};

interface HashrateValueProps {
  datetime: number;
  hashrates: Hashrates;
}

const getHashrateValue = ({ datetime, hashrates }: HashrateValueProps) => {
  if (!hashrates.length) {
    return 0;
  }
  // ignore seconds, only match up to minute
  const matchedTime = hashrates.find(
    (hashrate) =>
      getDayFromEpoch(hashrate.datetime) === getDayFromEpoch(datetime) &&
      getTimeFromEpoch(hashrate.datetime).slice(0, -3) ===
      getTimeFromEpoch(datetime).slice(0, -3)
  );
  return matchedTime?.value || 0;
};

interface ChartDataProps {
  hashrate1: Hashrates;
  hashrate2: Hashrates;
  hashrate3: Hashrates;
  hashrates: Hashrates;
}

export const getChartData = ({
  hashrate1,
  hashrate2,
  hashrate3,
  hashrates,
}: ChartDataProps) => {
  const chartData = hashrates.map((hashrate) => {
    const hashrate1Value = getHashrateValue({
      datetime: hashrate.datetime,
      hashrates: hashrate1,
    });
    const hashrate2Value = getHashrateValue({
      datetime: hashrate.datetime,
      hashrates: hashrate2,
    });
    const hashrate3Value = getHashrateValue({
      datetime: hashrate.datetime,
      hashrates: hashrate3,
    });

    return {
      datetime: hashrate.datetime,
      hashrate1: hashrate1Value,
      hashrate2: hashrate2Value,
      hashrate3: hashrate3Value,
      totalHashrate: hashrate.value,
    };
  });

  return chartData || [];
};
