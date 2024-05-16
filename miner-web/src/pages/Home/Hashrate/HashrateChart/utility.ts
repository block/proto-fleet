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
  return hashrates?.find((hashrate) => hashrate.datetime === datetime)?.value;
};
