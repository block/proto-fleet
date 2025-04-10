import { TEMP_UNITS, THEMES } from "./constants";

export type Themes = keyof typeof THEMES;

export type ThemeColors = keyof Omit<typeof THEMES, "system">;

export type TemperatureUnits = keyof typeof TEMP_UNITS;
