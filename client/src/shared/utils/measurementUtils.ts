import { Measurement } from "@/protoFleet/api/generated/common/v1/measurement_pb";

/**
 * Gets the latest measurement with a valid value based on timestamp
 * @param measurements Array of measurements
 * @returns The latest measurement that has a value, or undefined if none found
 */
export const getLatestMeasurementWithData = (measurements: Measurement[] | undefined): Measurement | undefined => {
  if (!measurements || measurements.length === 0) return undefined;

  let latest: Measurement | undefined;

  for (const m of measurements) {
    if (m?.value !== undefined && m?.value !== null && m?.timestamp?.seconds !== undefined) {
      if (!latest || m.timestamp?.seconds > (latest.timestamp?.seconds ?? 0)) {
        latest = m;
      }
    }
  }

  return latest;
};
