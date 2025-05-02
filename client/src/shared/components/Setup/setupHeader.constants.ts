export const steps = {
  network: "network",
  authentication: "authentication",
  miners: "miners",
  miningPool: "miningPool",
  cooling: "cooling",
} as const;

export const protoOSSteps = [steps.authentication, steps.miningPool];

export const protoFleetSteps = [
  steps.authentication,
  steps.miners,
  steps.network,
  steps.miningPool,
];

export const stepNames = {
  [steps.network]: "Network",
  [steps.authentication]: "Authentication",
  [steps.miners]: "Miners",
  [steps.miningPool]: "Mining Pool",
  [steps.cooling]: "Cooling",
} as const;
