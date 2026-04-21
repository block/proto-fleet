/**
 * Generate a human-readable ASIC name based on total ASICs and index
 *
 * @param totalAsics - Total number of ASICs on the hashboard
 * @param asicIndex - Zero-based index of the ASIC
 * @returns ASIC name like "A0", "A1", "B0", "B1", etc.
 */
export const getAsicName = (totalAsics: number, asicIndex: number): string => {
  if (asicIndex < 0 || asicIndex >= totalAsics) {
    return `${asicIndex}`; // Fallback to index if out of range
  }

  const group = asicIndex >= totalAsics / 2 ? "B" : "A";
  const groupIndex = asicIndex % Math.floor(totalAsics / 2);

  return `${group}${groupIndex}`;
};
