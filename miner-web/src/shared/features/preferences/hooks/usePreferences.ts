import { useContext, useMemo } from "react";
import PreferencesContext from "../PreferencesContext";

const usePreferences = () => {
  const {
    theme,
    deviceTheme,
    temperatureUnits,
    setTheme,
    setTemperatureUnits,
  } = useContext(PreferencesContext);

  return useMemo(
    () => ({
      theme,
      temperatureUnits,
      deviceTheme,
      setTheme,
      setTemperatureUnits,
    }),
    [theme, temperatureUnits, deviceTheme, setTheme, setTemperatureUnits],
  );
};

export default usePreferences;
