export const performanceModes = {
  MaximumHashrate: "MaximumHashrate",
  Efficiency: "Efficiency",
} as const;

export type PerformanceMode =
  (typeof performanceModes)[keyof typeof performanceModes];

export const powerTargetModes = {
  default: "default",
  custom: "custom",
};

export type PowerTargetMode =
  (typeof powerTargetModes)[keyof typeof powerTargetModes];
