/**
 * Get a standardized ASIC ID by combining hashboard serial and ASIC ID
 * Format: {hashboardSerial}_ASIC_{asicId padded to 3 digits}
 * Example: "HB001_ASIC_042"
 */
export function getAsicId(hashboardSerial: string, asicId: number | string): string {
  return `${hashboardSerial}_ASIC_${String(asicId).padStart(3, "0")}`;
}
