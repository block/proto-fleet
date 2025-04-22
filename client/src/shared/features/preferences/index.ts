import { TEMP_UNITS } from "./constants";
import usePreferences from "./hooks/usePreferences";
import { PreferencesProvider } from "./PreferencesContext";
import TemperatureUnitsSwitcher from "./TemperatureUnitsSwitcher";
import ThemeSwitcher from "./ThemeSwitcher";
import type { TemperatureUnits, Themes } from "./types";

export {
  PreferencesProvider,
  ThemeSwitcher,
  TemperatureUnitsSwitcher,
  TEMP_UNITS,
  usePreferences,
  type Themes,
  type TemperatureUnits,
};
