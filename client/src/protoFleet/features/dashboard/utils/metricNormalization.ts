/**
 * Normalizes telemetry metric values into dashboard display units.
 *
 * The API should already return display units, but older/mixed datasets can
 * contain raw storage units. These guards keep charts resilient across both.
 */

const HASHRATE_RAW_PER_DEVICE_THRESHOLD_HS = 1e9; // raw H/s is orders of magnitude larger than TH/s

const POWER_RAW_PER_DEVICE_THRESHOLD_W = 100; // miners in raw watts are typically well above this

const EFFICIENCY_RAW_THRESHOLD_JH = 1e-6; // raw J/H values are tiny (e.g. 24e-12)
const EFFICIENCY_OVER_CONVERTED_THRESHOLD_JTH = 1e6; // accidentally converted values become astronomically large

const hasValidDeviceCount = (deviceCount: number): boolean => {
  return Number.isFinite(deviceCount) && deviceCount > 0;
};

export const normalizeHashrateToTHs = (value: number, deviceCount: number): number => {
  if (!Number.isFinite(value) || !hasValidDeviceCount(deviceCount)) return value;

  const perDevice = Math.abs(value) / deviceCount;

  if (perDevice > HASHRATE_RAW_PER_DEVICE_THRESHOLD_HS) {
    return value / 1e12;
  }

  return value;
};

export const normalizePowerToKW = (value: number, deviceCount: number): number => {
  if (!Number.isFinite(value) || !hasValidDeviceCount(deviceCount)) return value;

  const perDevice = Math.abs(value) / deviceCount;

  if (perDevice > POWER_RAW_PER_DEVICE_THRESHOLD_W) {
    return value / 1e3;
  }

  return value;
};

export const normalizeEfficiencyToJTH = (value: number): number => {
  if (!Number.isFinite(value)) return value;

  const absValue = Math.abs(value);

  if (absValue > 0 && absValue <= EFFICIENCY_RAW_THRESHOLD_JH) {
    return value * 1e12;
  }

  if (absValue >= EFFICIENCY_OVER_CONVERTED_THRESHOLD_JTH) {
    return value / 1e12;
  }

  return value;
};
