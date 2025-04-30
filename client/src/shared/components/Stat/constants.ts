export const chartStatus = {
  neutral: "neutral",
  warning: "warning",
  critical: "critical",
  success: "success",
};

export type ChartStatus = keyof typeof chartStatus;

export const statusColors = {
  [chartStatus.neutral]: "bg-text-primary-50",
  [chartStatus.warning]: "bg-core-accent-fill",
  [chartStatus.critical]: "bg-intent-critical-fill",
  [chartStatus.success]: "bg-intent-success-fill",
} as const;
