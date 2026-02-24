import { useMemo } from "react";

/**
 * Determines if a miner needs attention based on authentication, pool, device error status, or hardware errors
 *
 * @param needsAuthentication - Whether the miner requires authentication
 * @param needsMiningPool - Whether the miner needs mining pool configuration
 * @param errors - Array of hardware errors from the miner (any array type)
 * @param hasDeviceError - Whether the server reported DeviceStatus.ERROR for this device
 * @returns true if the miner needs attention, false otherwise
 */
export function useNeedsAttention(
  needsAuthentication: boolean,
  needsMiningPool: boolean,
  errors: unknown[] | undefined,
  hasDeviceError: boolean = false,
): boolean {
  return useMemo(() => {
    const hasHardwareErrors = !!errors && errors.length > 0;
    return needsAuthentication || needsMiningPool || hasHardwareErrors || hasDeviceError;
  }, [needsAuthentication, needsMiningPool, errors, hasDeviceError]);
}
