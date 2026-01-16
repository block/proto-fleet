import { useMemo } from "react";

/**
 * Determines if a miner needs attention based on authentication, pool, or hardware errors
 *
 * @param needsAuthentication - Whether the miner requires authentication
 * @param needsMiningPool - Whether the miner needs mining pool configuration
 * @param errors - Array of hardware errors from the miner (any array type)
 * @returns true if the miner needs attention, false otherwise
 */
export function useNeedsAttention(
  needsAuthentication: boolean,
  needsMiningPool: boolean,
  errors: unknown[] | undefined,
): boolean {
  return useMemo(() => {
    const hasHardwareErrors = !!errors && errors.length > 0;
    return needsAuthentication || needsMiningPool || hasHardwareErrors;
  }, [needsAuthentication, needsMiningPool, errors]);
}
