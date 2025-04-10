import { useCallback, useEffect, useMemo, useState } from "react";

import { THEMES } from "../constants";
import { ThemeColors, Themes } from "../types";

import { useLocalStorage } from "@/shared/hooks/useLocalStorage";

const useTheme = () => {
  const { getItem, setItem } = useLocalStorage();
  const [theme, setTheme] = useState<Themes>(getItem("theme") || THEMES.system);
  const [appTheme, setAppTheme] = useState<ThemeColors>();
  const [deviceTheme, setDeviceTheme] = useState<ThemeColors>();

  const getDeviceTheme = useCallback(
    (darkThemeMq: MediaQueryList | MediaQueryListEvent) => {
      return darkThemeMq.matches ? THEMES.dark : THEMES.light;
    },
    [],
  );

  const updateTheme = useCallback(
    (deviceTheme: ThemeColors) => {
      setDeviceTheme(deviceTheme);
      setAppTheme(theme === THEMES.system ? deviceTheme : theme);
    },
    [theme, setAppTheme],
  );

  useEffect(() => {
    const darkThemeMq = window.matchMedia("(prefers-color-scheme: dark)");
    const deviceTheme = getDeviceTheme(darkThemeMq);
    updateTheme(deviceTheme);

    const handleChangeDeviceTheme = (e: MediaQueryListEvent) => {
      const deviceTheme = getDeviceTheme(e);
      updateTheme(deviceTheme);
    };

    darkThemeMq.addEventListener("change", handleChangeDeviceTheme);

    return () => {
      darkThemeMq.removeEventListener("change", handleChangeDeviceTheme);
    };
  }, [getDeviceTheme, updateTheme]);

  useEffect(() => {
    if (!appTheme) {
      return;
    }

    document.body.setAttribute("data-theme", appTheme);
  }, [appTheme]);

  // store in local storage for persistency
  useEffect(() => {
    setItem("theme", theme);
  }, [theme, setItem]);

  return useMemo(
    () => ({
      theme,
      setTheme,
      deviceTheme,
    }),
    [theme, setTheme, deviceTheme],
  );
};

export default useTheme;
