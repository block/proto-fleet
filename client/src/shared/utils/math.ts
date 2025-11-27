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
 * @param a starting value
 * @param b ending value
 * @param t ratio between the two values, tbetween 0 and 1
 * @param clamp whether to clamp the result between a and b {default: true}
 * @returns value between a and b, where t=0 returns a and t=1 returns b
 */
export const lerp = (a: number, b: number, t: number, clamped: boolean = true): number => {
  const lerped = a + (b - a) * t;
  return clamped ? clamp(lerped, Math.min(a, b), Math.max(a, b)) : lerped;
};

/**
 * @param a starting value
 * @param b ending value
 * @param t value to normalize between a and b
 * @returns normalized value between 0 and 1, where a returns 0 and b returns 1
 */
export const invLerp = (a: number, b: number, t: number, clamped: boolean = true): number => {
  const invLerped = a + (b - a) * t;
  return clamped ? clamp(invLerped, Math.min(a, b), Math.max(a, b)) : invLerped;
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
