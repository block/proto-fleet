/**
 * Generates a subtitle showing how many miners are reporting data.
 * Only returns a subtitle when not all miners are reporting.
 *
 * @param deviceCount - Number of miners reporting this metric
 * @param totalMiners - Total number of miners in the fleet
 * @returns Subtitle string or undefined if all miners are reporting
 */
export function getMinerCountSubtitle(deviceCount: number | null, totalMiners: number): string | undefined {
  if (deviceCount === null || totalMiners <= 0 || deviceCount >= totalMiners) {
    return undefined;
  }

  return `${deviceCount} of ${totalMiners} miners reporting`;
}
