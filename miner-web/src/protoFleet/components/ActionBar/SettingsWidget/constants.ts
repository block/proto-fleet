export const settingsActions = {
  miningPool: "mining-pool",
  coolingMode: "cooling-mode",
  security: "security",
} as const;

export type SettingsAction =
  (typeof settingsActions)[keyof typeof settingsActions];
