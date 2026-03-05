// Device Actions
export const deviceActions = {
  blinkLEDs: "blink-leds",
  downloadLogs: "download-logs",
  factoryReset: "factory-reset",
  reboot: "reboot",
  shutdown: "shutdown",
  delete: "delete",
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
  rename: "rename",
  security: "security",
} as const;

export type SettingsAction = (typeof settingsActions)[keyof typeof settingsActions];

// Group Actions
export const groupActions = {
  addToGroup: "add-to-group",
} as const;

export type GroupAction = (typeof groupActions)[keyof typeof groupActions];

// All Actions Combined
export const allActions = {
  ...deviceActions,
  ...performanceActions,
  ...settingsActions,
  ...groupActions,
} as const;

export type SupportedAction = (typeof allActions)[keyof typeof allActions];

export const minersMessage = "miners";

export const loadingMessages: Record<string, string> = {
  [deviceActions.blinkLEDs]: "Blinking LEDs",
  [deviceActions.downloadLogs]: "Downloading logs",
  [deviceActions.factoryReset]: "Resetting",
  [deviceActions.reboot]: "Rebooting",
  [deviceActions.shutdown]: "Putting to sleep",
  [deviceActions.delete]: "Deleting",
  [deviceActions.wakeUp]: "Waking up",
  [performanceActions.managePower]: "Updating power settings for",
  [performanceActions.curtail]: "Curtailing miners",
  [settingsActions.miningPool]: "Assigning pools",
  [settingsActions.coolingMode]: "Setting cooling mode for",
  [settingsActions.rename]: "Renaming miner",
  [settingsActions.security]: "Updating security for",
  [groupActions.addToGroup]: "Adding to group",
};

export const statusColumnLoadingMessages: Record<string, string> = {
  [deviceActions.blinkLEDs]: "Blinking LEDs",
  [deviceActions.factoryReset]: "Resetting",
  [deviceActions.reboot]: "Rebooting",
  [deviceActions.shutdown]: "Sleeping",
  [deviceActions.delete]: "Deleting",
  [deviceActions.wakeUp]: "Waking",
  [performanceActions.curtail]: "Curtailing",
  [settingsActions.miningPool]: "Adding pools",
  [settingsActions.coolingMode]: "Setting cooling",
  [settingsActions.security]: "Updating security",
};

export const successMessages: Record<string, string> = {
  [deviceActions.blinkLEDs]: "Blinked LEDs",
  [deviceActions.downloadLogs]: "Downloaded logs",
  [deviceActions.factoryReset]: "Reset",
  [deviceActions.reboot]: "Rebooted",
  [deviceActions.shutdown]: "Put to sleep",
  [deviceActions.delete]: "Deleted",
  [deviceActions.wakeUp]: "Woke up",
  [performanceActions.managePower]: "Updated power settings for",
  [performanceActions.curtail]: "Miners curtailed",
  [settingsActions.miningPool]: "Assigned pools to",
  [settingsActions.coolingMode]: "Updated cooling mode for",
  [settingsActions.rename]: "Miner renamed",
  [settingsActions.security]: "Updated security for",
  [groupActions.addToGroup]: "Added to group",
};
