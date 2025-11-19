/**
 * Status types for composition bar segments
 */
export type Status = "OK" | "WARNING" | "CRITICAL" | "NA";

/**
 * Individual segment in the composition bar
 */
export interface Segment {
  /** Display name for the segment */
  name: string;
  /** Status determining the color */
  status: Status;
  /** Count value used to calculate percentage - undefined for loading state */
  count?: number;
}

/**
 * Props for the CompositionBar component
 */
export interface CompositionBarProps {
  /** Array of segments to display */
  segments: Segment[];
  /** Optional custom CSS classes */
  className?: string;
  /** Height of the bar in pixels (default: 8) */
  height?: number;
  /** Gap between segments (default: 2) - uses Tailwind gap classes (0-12) */
  gap?: number;
  /** Optional custom color mappings for status values */
  colorMap?: Partial<Record<Status, string>>;
}
