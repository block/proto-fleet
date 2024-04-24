import { getRandomFloat, getRandomInt } from "common/utils/utility";

export const getAsics = () => {
  const asics: Record<"temp_c" | "row" | "column" | "hashrate_ghs", number>[] = [];

  [...Array(10).keys()].map((row) => {
    [...Array(10).keys()].map((column) => {
      asics.push({
        temp_c: getRandomFloat(40, 60),
        hashrate_ghs: getRandomInt(10, 30),
        row,
        column,
      });
    });
  });

  return asics;
};

// TODO: update these when we know actual warning temps
export const warningTemp = 57.3;
export const dangerTemp = 58.4;
