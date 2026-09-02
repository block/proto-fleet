import type { Measurement } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { getLatestMeasurementWithData } from "@/shared/utils/measurementUtils";

// Stable reference for empty measurement array (prevents infinite re-renders when used in components)
const EMPTY_MEASUREMENT: Measurement[] = [];

/**
 * Pure function for resolving miner measurement display state.
 *
 * @param miner - The miner state snapshot (or undefined if not loaded)
 * @param measurementGetter - Function to extract the specific measurement from a miner
 * @returns Display state:
 *   - `undefined` — miner not loaded OR online with no data yet (show skeleton)
 *   - `null` — offline or inactive with no data (show dash placeholder)
 *   - `[]` — needs pool or auth (show empty cell)
 *   - `Measurement[]` — has valid data (show value)
 */
export function getMinerMeasurement(
  miner: MinerStateSnapshot | undefined,
  measurementGetter: (miner: MinerStateSnapshot) => Measurement[] | undefined,
): Measurement[] | null | undefined {
  if (!miner) return undefined;

  // Offline miners should always show placeholder, not stale cached values
  if (miner.deviceStatus === DeviceStatus.OFFLINE) {
    return null;
  }

  // Show empty cell for devices with pool required or auth required status
  const needsPool = miner.deviceStatus === DeviceStatus.NEEDS_MINING_POOL;
  const needsAuth = miner.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  if (needsPool || needsAuth) {
    return EMPTY_MEASUREMENT;
  }

  const measurementData = measurementGetter(miner);
  const hasValidData = measurementData && getLatestMeasurementWithData(measurementData);

  if (!hasValidData) {
    if (miner.deviceStatus === DeviceStatus.INACTIVE) {
      return null;
    }
    return undefined;
  }

  return measurementData;
}
