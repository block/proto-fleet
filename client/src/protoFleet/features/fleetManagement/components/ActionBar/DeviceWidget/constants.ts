export const deviceActions = {
  blinkLEDs: "blink-leds",
  downloadLogs: "download-logs",
  factoryReset: "factory-reset",
  reboot: "reboot",
  shutdown: "shutdown",
  wakeUp: "wake-up",
} as const;

export type DeviceAction = (typeof deviceActions)[keyof typeof deviceActions];
