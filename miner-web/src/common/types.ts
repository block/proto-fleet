import { themes } from "./constants";

export type Themes = keyof typeof themes;

export type ThemeColors = keyof Omit<typeof themes, "system">;
