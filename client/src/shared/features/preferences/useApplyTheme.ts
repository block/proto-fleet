import { useEffect } from "react";
import type { Theme, ThemeColor } from "./types";

interface UseApplyThemeProps {
  theme: Theme;
  deviceTheme: ThemeColor | undefined;
  setDeviceTheme: (theme: ThemeColor) => void;
}

/**
 * Hook that handles theme side effects:
 * - Listens to OS theme changes
 * - Updates deviceTheme via the provided setter
 * - Applies theme to document.body
 *
 * This should be called at the app root level to ensure theme is applied on mount.
 */
export const useApplyTheme = ({ theme, deviceTheme, setDeviceTheme }: UseApplyThemeProps) => {
  // Listen to OS theme changes
  useEffect(() => {
    const getDeviceTheme = (mq: MediaQueryList | MediaQueryListEvent): ThemeColor => {
      return mq.matches ? "dark" : "light";
    };

    const darkThemeMq = window.matchMedia("(prefers-color-scheme: dark)");
    const initialTheme = getDeviceTheme(darkThemeMq);
    setDeviceTheme(initialTheme);

    const handleChange = (e: MediaQueryListEvent) => {
      const newTheme = getDeviceTheme(e);
      setDeviceTheme(newTheme);
    };

    darkThemeMq.addEventListener("change", handleChange);
    return () => darkThemeMq.removeEventListener("change", handleChange);
  }, [setDeviceTheme]);

  // Apply theme to document.body
  useEffect(() => {
    const appTheme = theme === "system" ? deviceTheme : theme;
    if (appTheme) {
      document.body.setAttribute("data-theme", appTheme);
    }
  }, [theme, deviceTheme]);
};
