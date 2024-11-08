import { useContext } from "react";

import { ThemeContext } from "common/contexts/ThemeContext";

const useThemeContext = () => {
  const { deviceTheme, getUserSelectedTheme, setUserSelectedTheme } =
    useContext(ThemeContext);

  return {
    deviceTheme,
    getUserSelectedTheme,
    setUserSelectedTheme,
  };
};

export { useThemeContext };
