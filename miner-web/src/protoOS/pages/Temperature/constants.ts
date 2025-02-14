import { getRandomFloat } from "@/shared/utils/utility";

export const getAsics = () => {
  const asics: Record<
    "temp_c" | "row" | "column" | "hashrate_ghs" | "id",
    number
  >[] = [];

  [...Array(10).keys()].map((row) => {
    [...Array(10).keys()].map((column) => {
      asics.push({
        temp_c: getRandomFloat(40, 60),
        hashrate_ghs: getRandomFloat(0, 1),
        row,
        column,
        id: +`${row}${column}`,
      });
    });
  });

  return asics;
};

// TODO: update these when we know actual warning temps
export const warningTemp = 85;
export const dangerTemp = 90;
