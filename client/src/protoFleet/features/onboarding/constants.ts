// Canonical scan ports for the default onboarding scan.
// Keep this list curated instead of inheriting every plugin-advertised port so
// the default flow does not fan out into duplicate discoveries for drivers that
// probe by IP rather than by scanned port.
export const protoRigPorts = ["443", "8080"];
export const antminerPort = "4028";
export const defaultDiscoveryPorts = [...protoRigPorts, antminerPort];

export const defaultTimeout = 10;

export const STEP_KEYS = {
  miners: "miners",
  security: "security",
  settings: "settings",
};

export const STEPS = {
  [STEP_KEYS.miners]: {
    label: "Miners",
    statusIndicator: "devicePaired",
  },
  [STEP_KEYS.security]: {
    label: "Security",
    // TODO: onboardingStatus does not yet include securityConfigured
    // faking it for now with devicePaired
    statusIndicator: "devicePaired",
  },
  [STEP_KEYS.settings]: {
    label: "Settings",
    // TODO: because cooling mode will eventually be included in this view
    // should we rename the OnboardingStatus key to something more generic?
    statusIndicator: "poolConfigured",
  },
} as const;
