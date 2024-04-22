const precision = 100;

export const getAsics = () => {
  const asics: Record<"temp_c" | "row" | "col" | "hashrate_ghs", number>[] = [];

  [...Array(10).keys()].map((row) => {
    [...Array(10).keys()].map((col) => {
      asics.push({
        temp_c:
          (Math.floor(
            Math.random() * (60 * precision - 40 * precision) + 1 * precision
          ) +
            40 * precision) /
          (1 * precision),
        hashrate_ghs: Math.floor(Math.random() * (30 - 10 + 1)) + 10,
        row,
        col,
      });
    });
  });

  return asics;
};

// TODO: update these when we know actual warning temps
export const warningTemp = 57.3;
export const dangerTemp = 58.4;
