import { defaultHashboardColor, hashboardColors } from "./constants";

export const getHashboardColor = (slot: number | null) => {
  if (slot === null) return defaultHashboardColor;
  return hashboardColors[(slot - 1) % hashboardColors.length];
};
