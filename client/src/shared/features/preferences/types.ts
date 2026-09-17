export type Theme = "dark" | "light" | "system";

export type ThemeColor = Exclude<Theme, "system">;

export type TemperatureUnit = "C" | "F";

export type FlowUnit = "GPM" | "L/min";
