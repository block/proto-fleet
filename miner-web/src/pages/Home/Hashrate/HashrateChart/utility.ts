import { Hashrates } from "../types";

export const getPoint = (index: number, firstPoint: number, gap: number) => {
  return firstPoint + index * gap;
};

interface HashrateValueProps {
  datetime: string;
  hashrates: Hashrates;
}

export const getHashrateValue = ({
  datetime,
  hashrates,
}: HashrateValueProps) => {
  // ignore seconds, only match up to minute
  return hashrates?.find(
    (hashrate) =>
      hashrate.datetime.toString().slice(0, -3) ===
      datetime.toString().slice(0, -3)
  )?.value;
};
