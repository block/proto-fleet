// Device Actions
export const deviceActions = {
  blinkLEDs: "blink-leds",
  downloadLogs: "download-logs",
  factoryReset: "factory-reset",
  reboot: "reboot",
  shutdown: "shutdown",
  unpair: "unpair",
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

export const minersMessage = "miners";

export const loadingMessages: Record<string, string> = {
  [deviceActions.blinkLEDs]: "Blinking LEDs",
  [deviceActions.factoryReset]: "Resetting",
  [deviceActions.reboot]: "Rebooting",
  [deviceActions.shutdown]: "Shutting down",
  [deviceActions.unpair]: "Unpairing",
  [deviceActions.wakeUp]: "Waking up",
  [performanceActions.curtail]: "Curtailing miners",
};

export const successMessages: Record<string, string> = {
  [deviceActions.blinkLEDs]: "Blinked LEDs",
  [deviceActions.factoryReset]: "Reset",
  [deviceActions.reboot]: "Rebooted",
  [deviceActions.shutdown]: "Shut down",
  [deviceActions.unpair]: "Unpaired",
  [deviceActions.wakeUp]: "Woke up",
  [performanceActions.curtail]: "Miners curtailed",
};
