import { useCallback, useEffect, useMemo, useState } from "react";

import { themes } from "../constants";
import { ThemeColors, Themes } from "../types";

import { useLocalStorage } from "@/shared/hooks/useLocalStorage";

const useThemes = () => {
  const [deviceTheme, setDeviceTheme] = useState<ThemeColors>(themes.light);
  const { getItem, setItem } = useLocalStorage();

  const getDeviceTheme = useCallback(
    (darkThemeMq: MediaQueryList | MediaQueryListEvent) => {
      return darkThemeMq.matches ? themes.dark : themes.light;
    },
    [],
  );

  const getUserSelectedTheme = useCallback(() => {
    return getItem("theme") || themes.system;
  }, [getItem]);

  const setAppTheme = useCallback((theme: ThemeColors) => {
    document.body.setAttribute("data-theme", theme);
  }, []);

  const setUserSelectedTheme = useCallback(
    (theme: Themes) => {
      setItem("theme", theme);
      if (theme !== themes.system) {
        setAppTheme(theme);
      } else {
        setAppTheme(deviceTheme);
      }
    },
    [deviceTheme, setItem, setAppTheme],
  );

  const handleChangeDeviceTheme = useCallback(
    (theme: ThemeColors) => {
      setDeviceTheme(theme);
      const userSelectedTheme = getUserSelectedTheme();
      setAppTheme(
        userSelectedTheme === themes.system ? theme : userSelectedTheme,
      );
    },
    [getUserSelectedTheme, setAppTheme],
  );

  useEffect(() => {
    const darkThemeMq = window.matchMedia("(prefers-color-scheme: dark)");
    const theme = getDeviceTheme(darkThemeMq);
    handleChangeDeviceTheme(theme);

    darkThemeMq.addEventListener("change", (e) => {
      const theme = getDeviceTheme(e);
      handleChangeDeviceTheme(theme);
    });
  }, [getDeviceTheme, handleChangeDeviceTheme]);

  return useMemo(
    () => ({
      deviceTheme,
      getUserSelectedTheme,
      setUserSelectedTheme,
    }),
    [deviceTheme, getUserSelectedTheme, setUserSelectedTheme],
  );
};

export { useThemes };
