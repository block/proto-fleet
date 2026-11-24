/** Prefer reported J/TH, otherwise calculate watts / TH/s from valid readings. */
export const efficiencyValue = (
  power: number | undefined,
  hashrate: number | undefined,
  reported?: number | null,
): number | null => {
  if (reported != null && Number.isFinite(reported) && reported >= 0) return reported;
  if (
    power === undefined ||
    !Number.isFinite(power) ||
    power < 0 ||
    hashrate === undefined ||
    !Number.isFinite(hashrate) ||
    hashrate <= 0
  )
    return null;
  const calculated = power / hashrate;
  return Number.isFinite(calculated) ? calculated : null;
};
