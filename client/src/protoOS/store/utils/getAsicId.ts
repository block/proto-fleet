import { asAsicId, type AsicId } from "../contract";

/**
 * Get the canonical ASIC ID for a hashboard serial and device-local index.
 * Format: serial:{hashboardSerial}:asic:{asicIndex}
 * Example: "serial:HB001:asic:42"
 */
export function getAsicId(hashboardSerial: string, asicId: number | string): AsicId {
  const hashboardId = hashboardSerial.startsWith("serial:") ? hashboardSerial : `serial:${hashboardSerial}`;
  return asAsicId(`${hashboardId}:asic:${asicId}`);
}
