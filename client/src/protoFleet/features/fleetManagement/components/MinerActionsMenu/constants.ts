// Device Actions
export const deviceActions = {
  blinkLEDs: "blink-leds",
  downloadLogs: "download-logs",
  factoryReset: "factory-reset",
  reboot: "reboot",
  shutdown: "shutdown",
  wakeUp: "wake-up",
} as const;

export type DeviceAction = (typeof deviceActions)[keyof typeof deviceActions];

// Performance Actions
export const performanceActions = {
  performanceMode: "performance-mode",
  curtail: "curtail",
} as const;

export type PerformanceAction =
  (typeof performanceActions)[keyof typeof performanceActions];

// Settings Actions
export const settingsActions = {
  miningPool: "mining-pool",
  coolingMode: "cooling-mode",
  security: "security",
} as const;

export type SettingsAction =
  (typeof settingsActions)[keyof typeof settingsActions];

// All Actions Combined
export const allActions = {
  ...deviceActions,
  ...performanceActions,
  ...settingsActions,
} as const;

export type SupportedAction = (typeof allActions)[keyof typeof allActions];
