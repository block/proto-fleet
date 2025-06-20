export const performanceModes = {
  MaximumHashrate: "MaximumHashrate",
  Efficiency: "Efficiency",
} as const;

export type PerformanceMode =
  (typeof performanceModes)[keyof typeof performanceModes];
