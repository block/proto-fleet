import { createContext, type ReactNode } from "react";

import { TEMP_UNITS, THEMES } from "./constants";
import useTemperatureUnits from "./hooks/useTemperatureUnits";
import useTheme from "./hooks/useTheme";
import type { TemperatureUnits, ThemeColors, Themes } from "./types";

const PreferencesContext = createContext({
  theme: THEMES.system as Themes,
  deviceTheme: undefined as ThemeColors | undefined,
  temperatureUnits: TEMP_UNITS.celcius as TemperatureUnits,
  setTheme: (theme: Themes) => {
    void theme;
  },
  setTemperatureUnits: (temperatureUnits: TemperatureUnits) => {
    void temperatureUnits;
  },
});

export const PreferencesProvider = ({ children }: { children: ReactNode }) => {
  const { theme, setTheme, deviceTheme } = useTheme();
  const { temperatureUnits, setTemperatureUnits } = useTemperatureUnits();

  return (
    <PreferencesContext.Provider
      value={{
        theme,
        temperatureUnits,
        deviceTheme,
        setTheme,
        setTemperatureUnits,
      }}
    >
      {children}
    </PreferencesContext.Provider>
  );
};

export default PreferencesContext;
