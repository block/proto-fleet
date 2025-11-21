export const BAR_ANIMATION_DURATION = 1000;
export const DEFAULT_BAR_WIDTH = 12; // 12px width (w-3 in Tailwind)
export const DEFAULT_CHART_HEIGHT = 300;
export const DEFAULT_Y_AXIS_TICK_COUNT = 3; // Default number of horizontal grid lines
export const Y_AXIS_TICK_WIDTH = 50;
export const TOOLTIP_WIDTH = 269;
export const TOOLTIP_OFFSET = 24;

export const defaultColors = [
  "--color-extended-navy-fill",
  "--color-surface-5",
  "--color-intent-warning-fill",
  "--color-intent-critical-fill",
  "--color-extended-pink-fill",
  "--color-extended-purple-fill",
  "--color-extended-forest-fill",
  "--color-extended-teal-fill",
];

export const barProps = {
  strokeWidth: 0,
  isAnimationActive: true,
  animationDuration: BAR_ANIMATION_DURATION,
  animationEasing: "cubic-bezier(0.33, 1, 0.68, 1)" as const, // easeOutCubic
};

export const xAxisProps = {
  stroke: "var(--color-border-5)",
  strokeWidth: 1,
  tickMargin: 12, // 12px below baseline
  axisLine: false, // Use grid line instead to avoid doubling
  tickLine: false,
};

export const yAxisProps = {
  axisLine: false,
  strokeWidth: 1,
  tickLine: false,
  tick: { fontSize: 0 }, // Hide tick labels but keep the grid lines
  domain: [0, "dataMax"] as [number, string],
  width: 0, // No width needed since labels are hidden
};

export const cartesianGridProps = {
  horizontal: true,
  vertical: false,
  stroke: "var(--color-border-5)",
  strokeWidth: 1,
};
