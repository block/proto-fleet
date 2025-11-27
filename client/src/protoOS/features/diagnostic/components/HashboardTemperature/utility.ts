// import { AsicStats, HashboardInfo } from "@/protoOS/api/generatedApi";
import { AsicData } from "@/protoOS/store";

export const sortAsics = (asics: AsicData[]) => {
  return asics.sort((a, b) => {
    if (a.row === b.row) {
      return (a.column || 0) - (b.column || 0);
    }

    return (a.row || 0) - (b.row || 0);
  });
};

// returns the unique rows from the asics that have position data
// e.g. [0, 1, 2, 3, 4, 5, 6, 7, 8, 9]
export const getAsicsRows = (asics: AsicData[]) => {
  return [
    ...new Set(
      asics.filter((asic) => asic.row !== undefined && asic.column !== undefined).map((asic) => asic.row as number),
    ),
  ];
};

const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";

export const getRowLabel = (row: number) => {
  return alphabet.charAt(row);
};
