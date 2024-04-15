export const getAsics = () => {
  const asics: Record<"temp_c" | "row" | "col", number>[] = [];

  [...Array(10).keys()].map((row) => {
    [...Array(10).keys()].map((col) => {
      asics.push({
        temp_c: Math.floor(Math.random() * (60 - 40 + 1)) + 40,
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
