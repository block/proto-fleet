export const performanceActions = {
  performanceMode: "performance-mode",
  curtail: "curtail",
};

export type PerformanceAction =
  (typeof performanceActions)[keyof typeof performanceActions];
