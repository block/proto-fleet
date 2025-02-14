import { createContext } from "react";

import { themes } from "./constants";
import { ThemeColors, Themes } from "./types";

export const ThemeContext = createContext({
  deviceTheme: themes.light as ThemeColors,
  getUserSelectedTheme: () => themes.system as Themes,
  setUserSelectedTheme: (theme: Themes) => {
    void theme;
  },
});
