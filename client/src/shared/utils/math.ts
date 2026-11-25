/**
 * @param value value to clamp
 * @param min minimum value
 * @param max maximum value
 * @returns clamped value between min and max, where min returns min and max returns max
 */
export const clamp = (value: number, min: number, max: number): number => {
  return Math.max(min, Math.min(max, value));
};

/**
 * @param value value to map
 * @param inMin minimum value for input range
 * @param inMax maximum value for output range
 * @param outMin minimum value for output range
 * @param outMax maximum value for output range
 * @param clamped whether to clamp the result between outMin and outMax {default: true}
 * @returns mapped value between outMin and outMax, where inMin returns outMin and inMax returns outMax
 */
export const map = (
  value: number,
  inMin: number,
  inMax: number,
  outMin: number,
  outMax: number,
  clamped: boolean = true,
): number => {
  const mapped = ((value - inMin) * (outMax - outMin)) / (inMax - inMin) + outMin;
  return clamped ? clamp(mapped, Math.min(outMin, outMax), Math.max(outMin, outMax)) : mapped;
};
