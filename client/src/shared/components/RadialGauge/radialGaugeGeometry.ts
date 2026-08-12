export interface RadialGaugeGeometryInput {
  value: number;
  size: number;
  strokeWidth: number;
  sweep: number;
}

export interface RadialGaugeGeometry {
  radius: number;
  circumference: number;
  /** Dash length of the (low-opacity) track arc. */
  trackLength: number;
  /** Remaining circumference after the track arc. */
  trackGap: number;
  /** Dash length of the coloured value arc. */
  valueLength: number;
  /** Remaining circumference after the value arc. */
  valueGap: number;
  /** SVG rotation (deg) so the arc starts at top, centred for a partial sweep. */
  rotation: number;
}

/**
 * Pure geometry for the radial gauge: clamps value to 0–100 and sweep to
 * 1–360, then derives the stroke-dasharray lengths and rotation. The value
 * arc fills the same start point as the track up to `value`% of the sweep.
 */
export function getRadialGaugeGeometry({
  value,
  size,
  strokeWidth,
  sweep,
}: RadialGaugeGeometryInput): RadialGaugeGeometry {
  const clamped = Math.max(0, Math.min(100, value));
  const clampedSweep = Math.max(1, Math.min(360, sweep));

  const radius = (size - strokeWidth) / 2;
  const circumference = 2 * Math.PI * radius;
  const trackLength = (clampedSweep / 360) * circumference;
  const trackGap = circumference - trackLength;
  const valueLength = (clamped / 100) * trackLength;
  const valueGap = circumference - valueLength;
  const rotation = -90 - (360 - clampedSweep) / 2;

  return { radius, circumference, trackLength, trackGap, valueLength, valueGap, rotation };
}
