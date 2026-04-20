// Default color mappings for common status types
export const STATUS_COLORS = {
  // Uptime statuses
  hashing: "--color-intent-success-fill",
  notHashing: "--color-text-primary-20",

  // Temperature statuses
  normal: "--color-intent-success-fill",
  hot: "--color-intent-warning-fill",
  critical: "--color-intent-critical-fill",
  cold: "--color-intent-info-fill",
} as const;

export const DEFAULT_CHART_HEIGHT = 284;
