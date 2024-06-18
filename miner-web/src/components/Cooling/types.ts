import { fanModes } from "./constants";

export type FanMode = typeof fanModes[keyof typeof fanModes];
