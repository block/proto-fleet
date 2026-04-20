// Device Actions
export const deviceActions = {
  blinkLEDs: "blink-leds",
  downloadLogs: "download-logs",
  firmwareUpdate: "firmware-update",
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
  rename: "rename",
  updateWorkerNames: "update-worker-names",
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
  [deviceActions.unpair]: "Unpairing",
  [deviceActions.wakeUp]: "Waking up",
  [deviceActions.firmwareUpdate]: "Updating firmware on",
  [performanceActions.managePower]: "Updating power settings for",
  [performanceActions.curtail]: "Curtailing miners",
  [settingsActions.miningPool]: "Assigning pools",
  [settingsActions.coolingMode]: "Setting cooling mode for",
  [settingsActions.rename]: "Renaming miner",
  [settingsActions.updateWorkerNames]: "Updating worker names for",
  [settingsActions.security]: "Updating security for",
  [groupActions.addToGroup]: "Adding to group",
};

export const statusColumnLoadingMessages: Record<string, string> = {
  [deviceActions.blinkLEDs]: "Blinking LEDs",
  [deviceActions.factoryReset]: "Resetting",
  [deviceActions.reboot]: "Rebooting",
  [deviceActions.shutdown]: "Sleeping",
  [deviceActions.unpair]: "Unpairing",
  [deviceActions.wakeUp]: "Waking",
  [deviceActions.firmwareUpdate]: "Updating firmware",
  [performanceActions.managePower]: "Updating power",
  [performanceActions.curtail]: "Curtailing",
  [settingsActions.miningPool]: "Adding pools",
  [settingsActions.coolingMode]: "Setting cooling",
  [settingsActions.updateWorkerNames]: "Updating worker names",
  [settingsActions.security]: "Updating security",
};

export const successMessages: Record<string, string> = {
  [deviceActions.blinkLEDs]: "Blinked LEDs",
  [deviceActions.downloadLogs]: "Downloaded logs",
  [deviceActions.factoryReset]: "Reset",
  [deviceActions.reboot]: "Rebooted",
  [deviceActions.shutdown]: "Put to sleep",
  [deviceActions.unpair]: "Unpaired",
  [deviceActions.wakeUp]: "Woke up",
  [deviceActions.firmwareUpdate]: "Firmware installed on",
  [performanceActions.managePower]: "Updated power settings for",
  [performanceActions.curtail]: "Miners curtailed",
  [settingsActions.miningPool]: "Assigned pools to",
  [settingsActions.coolingMode]: "Updated cooling mode for",
  [settingsActions.rename]: "Miner renamed",
  [settingsActions.updateWorkerNames]: "Updated worker names for",
  [settingsActions.security]: "Updated security for",
  [groupActions.addToGroup]: "Added to group",
};

export const failureMessages: Record<string, string> = {
  [deviceActions.blinkLEDs]: "LED blink failed on",
  [deviceActions.downloadLogs]: "Log download failed on",
  [deviceActions.factoryReset]: "Reset failed on",
  [deviceActions.reboot]: "Reboot failed on",
  [deviceActions.shutdown]: "Sleep failed on",
  [deviceActions.unpair]: "Unpairing failed on",
  [deviceActions.wakeUp]: "Wake up failed on",
  [deviceActions.firmwareUpdate]: "Firmware update failed on",
  [performanceActions.managePower]: "Power update failed on",
  [performanceActions.curtail]: "Curtailment failed on",
  [settingsActions.miningPool]: "Pool assignment failed on",
  [settingsActions.coolingMode]: "Cooling mode update failed on",
  [settingsActions.rename]: "Renaming failed on",
  [settingsActions.updateWorkerNames]: "Worker name update failed on",
  [settingsActions.security]: "Security update failed on",
  [groupActions.addToGroup]: "Group assignment failed on",
};

export const getLoadingMessage = (action: SupportedAction, subject: string): string => {
  if (action === deviceActions.shutdown) return `Putting ${subject} to sleep`;
  const message = loadingMessages[action] ?? "Processing";
  return `${message} ${subject}`;
};

export const getSuccessMessage = (action: SupportedAction, subject: string): string => {
  if (action === deviceActions.shutdown) return `Put ${subject} to sleep`;
  const message = successMessages[action] ?? "Completed";
  return `${message} ${subject}`;
};

export const getFailureMessage = (action: SupportedAction, context: string): string => {
  const message = failureMessages[action] ?? "Action failed on";
  return `${message} ${context}`;
};
