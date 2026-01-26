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
  managePower: "manage-power",
  curtail: "curtail",
} as const;

export type PerformanceAction = (typeof performanceActions)[keyof typeof performanceActions];

// Settings Actions
export const settingsActions = {
  miningPool: "mining-pool",
  coolingMode: "cooling-mode",
  security: "security",
} as const;

export type SettingsAction = (typeof settingsActions)[keyof typeof settingsActions];

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
  [deviceActions.shutdown]: "Putting to sleep",
  [deviceActions.unpair]: "Unpairing",
  [deviceActions.wakeUp]: "Waking up",
  [performanceActions.managePower]: "Updating power settings for",
  [performanceActions.curtail]: "Curtailing miners",
  [settingsActions.miningPool]: "Assigning pools",
};

export const statusColumnLoadingMessages: Record<string, string> = {
  [deviceActions.blinkLEDs]: "Blinking LEDs",
  [deviceActions.factoryReset]: "Resetting",
  [deviceActions.reboot]: "Rebooting",
  [deviceActions.shutdown]: "Sleeping",
  [deviceActions.unpair]: "Unpairing",
  [deviceActions.wakeUp]: "Waking",
  [performanceActions.curtail]: "Curtailing",
  [settingsActions.miningPool]: "Adding pools",
};

export const successMessages: Record<string, string> = {
  [deviceActions.blinkLEDs]: "Blinked LEDs",
  [deviceActions.factoryReset]: "Reset",
  [deviceActions.reboot]: "Rebooted",
  [deviceActions.shutdown]: "Put to sleep",
  [deviceActions.unpair]: "Unpaired",
  [deviceActions.wakeUp]: "Woke up",
  [performanceActions.managePower]: "Updated power settings for",
  [performanceActions.curtail]: "Miners curtailed",
  [settingsActions.miningPool]: "Assigned pools to",
};
