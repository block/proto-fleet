import { AsicStats } from "apiTypes";

export const sortAsics = (asics: AsicStats[]) => {
  return asics.sort((a, b) => {
    if (a.row === b.row) {
      return (a.col || 0) - (b.col || 0);
    }

    return (a.row || 0) - (b.row || 0);
  });
};

// returns the unique rows from the asics
// e.g. [0, 1, 2, 3, 4, 5, 6, 7, 8, 9]
export const getAsicsRows = (asics: AsicStats[]) => {
  return [...new Set(asics.map((asic) => asic.row))];
};

const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";

export const getRowLabel = (row: number) => {
  return alphabet.charAt(row);
};
