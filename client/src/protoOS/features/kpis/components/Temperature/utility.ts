import { AsicStats, HashboardInfo } from "@/protoOS/api/types";

export const sortAsics = (asics: AsicStats[]) => {
  return asics.sort((a, b) => {
    if (a.row === b.row) {
      return (a.column || 0) - (b.column || 0);
    }

    return (a.row || 0) - (b.row || 0);
  });
};

export const sortHashboards = (hashboards: HashboardInfo[]) => {
  return hashboards.sort((a, b) => {
    const aSerial = a.hb_sn || "";
    const bSerial = b.hb_sn || "";
    return aSerial.localeCompare(bSerial);
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
