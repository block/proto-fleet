import type { Status } from "./types";

/**
 * Default height for the composition bar in pixels
 */
export const DEFAULT_BAR_HEIGHT = 8;

/**
 * Default gap between segments (Tailwind gap value)
 */
export const DEFAULT_GAP = 1;

/**
 * Map of gap values to Tailwind class names
 * Required because Tailwind doesn't support dynamic class generation
 */
export const GAP_CLASS_MAP: Record<number, string> = {
  0: "gap-0",
  1: "gap-1",
  2: "gap-2",
  3: "gap-3",
  4: "gap-4",
  5: "gap-5",
  6: "gap-6",
  8: "gap-8",
} as const;

/**
 * Minimum width percentage for a segment to ensure visibility
 */
export const MIN_SEGMENT_WIDTH_PERCENTAGE = 1;

/**
 * Color mappings for each status type
 * Using existing theme tokens from the design system
 */
export const STATUS_COLORS: Record<Status, string> = {
  OK: "bg-intent-success-fill",
  WARNING: "bg-intent-warning-fill",
  CRITICAL: "bg-intent-critical-fill",
  NA: "bg-grayscale-gray-50",
} as const;
