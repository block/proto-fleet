// based on rust enum AsicType
export const InternalAsicType = {
  CpuSimulated: "CpuSimulated",
  BZM2: "BZM2",
  MC1: "MC1",
  MC2: "MC2",
  MC2Sim: "MC2Sim",
} as const;

export const ExternalAsicType = {
  ChipSim: "Chip Sim",
  Chip1: "Chip 1",
  Chip2: "Chip 2",
  Chip2Sim: "Chip 2 Sim",
} as const;
