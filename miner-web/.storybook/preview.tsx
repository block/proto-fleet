import React from "react";
import type { Preview } from "@storybook/react";
import { useDarkMode } from "storybook-dark-mode";

import { ThemeContext, useThemes } from "../src/shared/features/themes";

import "../src/shared/styles/index.css";

export const decorators = [
  (Story) => {
    const isDark = useDarkMode();
    const { deviceTheme, setUserSelectedTheme, getUserSelectedTheme } =
      useThemes();

    React.useEffect(() => {
      const body = document.querySelector("body");
      body?.setAttribute("data-theme", isDark ? "dark" : "light");
    }, [isDark]);

    return (
      <ThemeContext.Provider
        value={{ deviceTheme, getUserSelectedTheme, setUserSelectedTheme }}
      >
        <Story />
      </ThemeContext.Provider>
    );
  },
];

const preview: Preview = {
  parameters: {
    actions: { argTypesRegex: "^on[A-Z].*" },
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
    darkMode: {
      current: "light",
    },
  },
};

export default preview;
