export const COOLING_MODES = {
  air: "air-cooled",
  immersion: "immersion-cooled",
} as const;

export type CoolingModeOption = (typeof COOLING_MODES)[keyof typeof COOLING_MODES];
