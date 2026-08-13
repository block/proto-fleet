import clsx from "clsx";
import { getRadialGaugeGeometry } from "./radialGaugeGeometry";

interface RadialGaugeProps {
  /** Progress value, 0–100. Clamped to the range. */
  value: number;
  /** Outer diameter in px. */
  size?: number;
  /** Ring thickness in px. */
  strokeWidth?: number;
  /**
   * Thickness of the coloured value arc in px. Defaults to `strokeWidth`; set
   * larger than `strokeWidth` to make the filled portion stand proud of the
   * track (the container fan-donut look).
   */
  valueStrokeWidth?: number;
  /**
   * Sweep of the track in degrees. 360 = full ring; a value like 270 leaves a
   * gap at the bottom for an open "gauge" look. Defaults to a full ring.
   */
  sweep?: number;
  /** Tailwind text-color class applied to the value arc via currentColor. */
  colorClassName?: string;
  /**
   * Line cap of the arcs. "round" reads softer for open gauges; "butt" gives
   * the flat-ended donut look used by the container fan cards.
   */
  strokeLinecap?: "round" | "butt";
  /** Opacity of the low-opacity track arc (0–1). */
  trackOpacity?: number;
  /** Large centred readout, e.g. "72%" or "3,200". */
  label?: string;
  /** Small caption under the label, e.g. "PWM" or "RPM". */
  caption?: string;
  className?: string;
  dataTestId?: string;
}

/**
 * A donut/ring gauge: a low-opacity track with a coloured value arc and an
 * optional centred readout. Extends the SVG-arc approach used by
 * ProgressCircular (which is a thin, label-less loading spinner) into a
 * value gauge for surfaces like the container fan cards.
 */
const RadialGauge = ({
  value,
  size = 96,
  strokeWidth = 8,
  valueStrokeWidth,
  sweep = 360,
  colorClassName = "text-core-accent-fill",
  strokeLinecap = "round",
  trackOpacity = 0.1,
  label,
  caption,
  className,
  dataTestId,
}: RadialGaugeProps) => {
  const valueStroke = valueStrokeWidth ?? strokeWidth;
  // Size the radius for the thickest arc so a proud value arc never clips the
  // viewBox; both arcs share this radius (a common centre line) so the value
  // arc stays a correct fraction of the track and starts at the same point.
  const geometryStroke = Math.max(strokeWidth, valueStroke);
  const { radius, trackLength, trackGap, valueLength, valueGap, rotation } = getRadialGaugeGeometry({
    value,
    size,
    strokeWidth: geometryStroke,
    sweep,
  });

  return (
    <div className={clsx("relative inline-flex items-center justify-center", className)} data-testid={dataTestId}>
      <svg
        xmlns="http://www.w3.org/2000/svg"
        width={size}
        height={size}
        viewBox={`0 0 ${size} ${size}`}
        fill="none"
        className={colorClassName}
      >
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke="currentColor"
          opacity={trackOpacity}
          strokeWidth={strokeWidth}
          strokeLinecap={strokeLinecap}
          strokeDasharray={`${trackLength} ${trackGap}`}
          transform={`rotate(${rotation} ${size / 2} ${size / 2})`}
        />
        {valueLength > 0 ? (
          <circle
            cx={size / 2}
            cy={size / 2}
            r={radius}
            fill="none"
            stroke="currentColor"
            strokeWidth={valueStroke}
            strokeLinecap={strokeLinecap}
            strokeDasharray={`${valueLength} ${valueGap}`}
            transform={`rotate(${rotation} ${size / 2} ${size / 2})`}
          />
        ) : null}
      </svg>
      {label || caption ? (
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          {label ? <span className="text-heading-200 text-text-primary">{label}</span> : null}
          {caption ? <span className="text-100 text-text-primary-50">{caption}</span> : null}
        </div>
      ) : null}
    </div>
  );
};

export default RadialGauge;
export type { RadialGaugeProps };
